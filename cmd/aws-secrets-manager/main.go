package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
)

func main() {
	secretArn := os.Getenv("SECRET_ARN")
	var AWSRegion string

	if arn.IsARN(secretArn) {
		arnobj, _ := arn.Parse(secretArn)
		AWSRegion = arnobj.Region
	} else {
		fmt.Println("Not a valid ARN")
		os.Exit(1)
	}

	sess, err := session.NewSession()
	svc := secretsmanager.New(sess, &aws.Config{
		Region: aws.String(AWSRegion),
	})

	input := &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String(secretArn),
		VersionStage: aws.String("AWSCURRENT"),
	}

	result, err := svc.GetSecretValue(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case secretsmanager.ErrCodeResourceNotFoundException:
				fmt.Println(secretsmanager.ErrCodeResourceNotFoundException, aerr.Error())
			case secretsmanager.ErrCodeInvalidParameterException:
				fmt.Println(secretsmanager.ErrCodeInvalidParameterException, aerr.Error())
			case secretsmanager.ErrCodeInvalidRequestException:
				fmt.Println(secretsmanager.ErrCodeInvalidRequestException, aerr.Error())
			case secretsmanager.ErrCodeDecryptionFailure:
				fmt.Println(secretsmanager.ErrCodeDecryptionFailure, aerr.Error())
			case secretsmanager.ErrCodeInternalServiceError:
				fmt.Println(secretsmanager.ErrCodeInternalServiceError, aerr.Error())
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			// Print the error, cast err to awserr.Error to get the Code and
			// Message from an error.
			fmt.Println(err.Error())
		}
		return
	}
	// Decrypts secret using the associated KMS CMK.
	// Depending on whether the secret is a string or binary, one of these fields will be populated.
	var secretString, decodedBinarySecret string
	if result.SecretString != nil {
		secretString = *result.SecretString
		writeOutput(secretString)
	} else {
		decodedBinarySecretBytes := make([]byte, base64.StdEncoding.DecodedLen(len(result.SecretBinary)))
		len, err := base64.StdEncoding.Decode(decodedBinarySecretBytes, result.SecretBinary)
		if err != nil {
			fmt.Println("Base64 Decode Error:", err)
			return
		}
		decodedBinarySecret = string(decodedBinarySecretBytes[:len])
		writeOutput(decodedBinarySecret)
	}
}

func writeEnvFile(key, value string) {
	f, err := os.OpenFile("/tmp/secret", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		return
	}
	defer f.Close()

	f.WriteString(fmt.Sprintf("export %s=%s;\n", key, value))
}

func writeOutput(output string) {
	// coming in as json. parse and extract the key and value for
	// writing to temp file as a structure env file
	var uj map[string]string
	if err := json.Unmarshal([]byte(output), &uj); err != nil {
		return
	}

	f, err := os.OpenFile("/tmp/secret", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	// the json read in should only ever have 1 key value pair,
	// however, iterate over it just in case anyhow.
	for k, v := range uj {
		f.WriteString(
			fmt.Sprintf("export %s=%s;\n", k, v),
		)
	}

}
