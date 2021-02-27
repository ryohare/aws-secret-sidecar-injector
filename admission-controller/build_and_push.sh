export GOOS=linux

go build

docker build -t aws-secrets-manager-secret-adm-controller .
docker tag aws-secrets-manager-secret-adm-controller 664393803520.dkr.ecr.us-east-1.amazonaws.com/aws-secrets-manager-secret-adm-controller
docker push 664393803520.dkr.ecr.us-east-1.amazonaws.com/aws-secrets-manager-secret-adm-controller