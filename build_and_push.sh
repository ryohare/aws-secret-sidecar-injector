docker build -t aws-secrets-manager-secret-sidecar .
docker tag aws-secrets-manager-secret-sidecar 664393803520.dkr.ecr.us-east-1.amazonaws.com/aws-secrets-manager-secret-sidecar
docker push 664393803520.dkr.ecr.us-east-1.amazonaws.com/aws-secrets-manager-secret-sidecar
