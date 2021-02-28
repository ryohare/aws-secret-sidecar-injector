bash build_and_push.sh
helm uninstall sil
helm install sil ./secret-inject
kubectl delete deployment webserver
