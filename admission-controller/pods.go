/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

const (
	podsSidecarPatch string = `[
		{"op":"add", "path":"/spec/containers/-","value":{"image":"%v","name":"webhook-added-sidecar","volumeMounts":[{"name":"vol","mountPath":"/tmp"}],"resources":{}}}
	]`
)

// is the operation for patching for the init containers. Needs an array of init containers
// to be added to the incoming manifest
var initContainersShell string = `{"op":"add","path":"/spec/initContainers","value":[%s]},`

// Init container array entry with values to be added. Last entry needs the , stripped off
// takes 3 values, image name, a number for the container and annotation name from the
// the incoming manifest
var initContainerEntry string = `{"image":"%v","name":"secrets-init-container-%d","volumeMounts":[{"name":"secret-vol","mountPath":"/tmp"}],"env":[{"name": "SECRET_ARN","valueFrom": {"fieldRef": {"fieldPath": "metadata.annotations['%v']"}}}],"resources":{}},`

// this modification will the secrets in memory volume which each init container will populate
// and the main container will use to pull the secrets in.
var secretsMountPointPatch string = `{"op":"add","path":"/spec/volumes/-","value":{"emptyDir": {"medium": "Memory"},"name": "secret-vol"}}`

// only allow pods to pull images from specific registry.
func admitPods(ar v1.AdmissionReview) *v1.AdmissionResponse {
	klog.V(2).Info("admitting pods")
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request.Resource != podResource {
		err := fmt.Errorf("expect resource to be %s", podResource)
		klog.Error(err)
		return toV1AdmissionResponse(err)
	}

	raw := ar.Request.Object.Raw
	pod := corev1.Pod{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		klog.Error(err)
		return toV1AdmissionResponse(err)
	}
	reviewResponse := v1.AdmissionResponse{}
	reviewResponse.Allowed = true

	var msg string
	if v, ok := pod.Labels["webhook-e2e-test"]; ok {
		if v == "webhook-disallow" {
			reviewResponse.Allowed = false
			msg = msg + "the pod contains unwanted label; "
		}
		if v == "wait-forever" {
			reviewResponse.Allowed = false
			msg = msg + "the pod response should not be sent; "
			<-make(chan int) // Sleep forever - no one sends to this channel
		}
	}
	for _, container := range pod.Spec.Containers {
		if strings.Contains(container.Name, "webhook-disallow") {
			reviewResponse.Allowed = false
			msg = msg + "the pod contains unwanted container name; "
		}
	}
	if !reviewResponse.Allowed {
		reviewResponse.Result = &metav1.Status{Message: strings.TrimSpace(msg)}
	}
	return &reviewResponse
}

func processAnnotations(pod *corev1.Pod) string {
	var patch string
	initCount := 0
	for annotation, value := range pod.ObjectMeta.Annotations {
		// a note about the annotation
		// using SSM, its a key value store which always returns
		// the keys in the json form { "key": "value" }. So, when
		// we set this up, it gets exported as KEY=VALUE. So, the
		// annotation values after the main clause, dont matter as
		// log as they are unique. We can look to use them in the case
		// where we dont get a key,value pair back. But for now, just
		// ignoring them. K8s will enforce they are globally unique
		if strings.Contains(annotation, "secrets.k8s.aws") {

			// ignore the injector turn on flag
			if annotation == "secrets.k8s.aws/sidecarInjectorWebhook" {
				continue
			}
			klog.Info(value)
			patchPart := fmt.Sprintf(initContainerEntry, sidecarImage, initCount, annotation)
			patch += patchPart
			initCount++
			klog.Info(patchPart)
		}
	}

	// trim off the trailing ,
	patch = patch[:len(patch)-1]

	klog.Info(fmt.Sprintf("Patch Array: \n*****\n%s\n******"), patch)

	// put the array elements into the shell entry
	patch = fmt.Sprintf(initContainersShell, patch)

	klog.Info(fmt.Sprintf("Full Init Containers Entry: \n*****\n%s\n******"), patch)

	// prepend the open array into the patch
	patch = fmt.Sprintf("[%s", patch)

	klog.Info(fmt.Sprintf("Full Entry: \n*****\n%s\n******"), patch)

	// Add the mount patch once
	patch += secretsMountPointPatch

	klog.Info(fmt.Sprintf("Patch statement: \n*****\n%s\n******\n", patch))

	return patch
}

func mutatePods(ar v1.AdmissionReview) *v1.AdmissionResponse {
	shouldPatchPod := func(pod *corev1.Pod) bool {

		// loop over looking for the annotations needed to query
		// for secrets from SSM.
		secretFound := false
		for annotation := range pod.ObjectMeta.Annotations {
			if strings.Contains(annotation, "secrets.k8s.aws") {
				// ignore the injector turn on flag
				if annotation == "secrets.k8s.aws/sidecarInjectorWebhook" {
					continue
				}
				secretFound = true
			}
		}

		if !secretFound {
			return false
		}

		return !hasContainer(pod.Spec.InitContainers, "secrets-init-container")
	}
	return applyPodPatch(ar, shouldPatchPod, "")
}

func mutatePodsSidecar(ar v1.AdmissionReview) *v1.AdmissionResponse {
	if sidecarImage == "" {
		return &v1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status:  "Failure",
				Message: "No image specified by the sidecar-image parameter",
				Code:    500,
			},
		}
	}
	shouldPatchPod := func(pod *corev1.Pod) bool {
		return !hasContainer(pod.Spec.Containers, "webhook-added-sidecar")
	}
	return applyPodPatch(ar, shouldPatchPod, fmt.Sprintf(podsSidecarPatch, sidecarImage))
}

func hasContainer(containers []corev1.Container, containerName string) bool {
	for _, container := range containers {
		if container.Name == containerName {
			return true
		}
	}
	return false
}

func applyPodPatch(ar v1.AdmissionReview, shouldPatchPod func(*corev1.Pod) bool, patch1 string) *v1.AdmissionResponse {
	klog.V(2).Info("mutating pods")
	klog.Info("Mutating Pods")
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request.Resource != podResource {
		klog.Errorf("expect resource to be %s", podResource)
		return nil
	}

	raw := ar.Request.Object.Raw
	pod := corev1.Pod{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		klog.Error(err)
		return toV1AdmissionResponse(err)
	}

	reviewResponse := v1.AdmissionResponse{}
	reviewResponse.Allowed = true
	var patch string

	// Need to add the secrets mount to the "rea" containers in the pod spec.
	// The init containers where created with this mount point and the patch
	// already has the addition of the in memory volume for the secrets.
	if shouldPatchPod(&pod) {
		// if we should patch, we need to process the pod's annotations
		// to get a handle to the initial patch
		patch = processAnnotations(&pod)
		klog.Info(fmt.Sprintf("Pre Processed Patch info:\n*****\n%s\n******", patch))

		// generate a random mount location to mitigate LFI
		mountLocation := uuid.New()

		klog.Info(
			fmt.Sprintf("Will mount secrets in main conatiners to %s", mountLocation),
		)

		var path = "{\"op\": \"add\",\"path\": \"/spec/containers/"
		var value = fmt.Sprintf("/volumeMounts/-\",\"value\": {\"mountPath\": \"/tmp/%s\",\"name\": \"secret-vol\"}}", mountLocation)

		envPatch := `{"op":"add","path":"/spec/containers/%d/env/-","value":{"name":"SEC_LOC","value":"/tmp/%s"}}`
		var volMounts = ""
		var envPatches = ""

		// Apply secrets mount to each container in the main pod spec
		for i := range pod.Spec.Containers {
			klog.Info(fmt.Sprintf("container: %s", i))
			if i == 0 {
				volMounts = path + strconv.Itoa(i) + value
				envPatches = fmt.Sprintf(envPatch, i, mountLocation)
			} else {
				volMounts = volMounts + "," + path + strconv.Itoa(i) + value
				envPatches = envPatches + "," + fmt.Sprintf(envPatch, i, mountLocation)
			}
		}
		patch = patch + "," + volMounts + "," + envPatches + "]"
		klog.Info(fmt.Sprintf("Post Processed Patch info:\n*****\n%s\n******", patch))
		reviewResponse.Patch = []byte(patch)
		pt := v1.PatchTypeJSONPatch
		reviewResponse.PatchType = &pt
	}
	klog.Info(&reviewResponse)
	return &reviewResponse
}

// denySpecificAttachment denies `kubectl attach to-be-attached-pod -i -c=container1"
// or equivalent client requests.
func denySpecificAttachment(ar v1.AdmissionReview) *v1.AdmissionResponse {
	klog.V(2).Info("handling attaching pods")
	if ar.Request.Name != "to-be-attached-pod" {
		return &v1.AdmissionResponse{Allowed: true}
	}
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if e, a := podResource, ar.Request.Resource; e != a {
		err := fmt.Errorf("expect resource to be %s, got %s", e, a)
		klog.Error(err)
		return toV1AdmissionResponse(err)
	}
	if e, a := "attach", ar.Request.SubResource; e != a {
		err := fmt.Errorf("expect subresource to be %s, got %s", e, a)
		klog.Error(err)
		return toV1AdmissionResponse(err)
	}

	raw := ar.Request.Object.Raw
	podAttachOptions := corev1.PodAttachOptions{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &podAttachOptions); err != nil {
		klog.Error(err)
		return toV1AdmissionResponse(err)
	}
	klog.V(2).Info(fmt.Sprintf("podAttachOptions=%#v\n", podAttachOptions))
	if !podAttachOptions.Stdin || podAttachOptions.Container != "container1" {
		return &v1.AdmissionResponse{Allowed: true}
	}
	return &v1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Message: "attaching to pod 'to-be-attached-pod' is not allowed",
		},
	}
}
