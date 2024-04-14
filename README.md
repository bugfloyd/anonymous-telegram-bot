# Anonymous Telegram Chat Bot


### Init Terraform Backend
```shell
cd infra/init
terraform init

init terraform plan \
-var aws_region=<AWS_REGION>\
-var terraform_state_bucket=<S3_TERRAFORM_STATE_BUCKET_NAME> \
-var lambda_bucket=<S3_CODE_BUCKET_NAME> \
-out init.tfplan
```

### Build and Upload Lambda Bundle
```shell
cd bot

GOARCH=amd64 GOOS=linux go build -o bootstrap main.go
zip lambda_function.zip bootstrap
aws s3 cp lambda_function.zip s3://<S3_CODE_BUCKET_NAME>/lambda_function.zip
```

### Add terraform Variables File
Create a file named `infra/terraform.tfvars`:
```hcl
aws_region     = <AWS_REGION>
lambda_bucket  = <S3_CODE_BUCKET_NAME>
bot_token      = <TELEGRAM_BOT_TOKEN>
```

### Initialize Terraform
Create a file named `infra/backend_config.hcl`:
```hcl
bucket         = <S3_TERRAFORM_STATE_BUCKET_NAME>
region         = <AWS_REGION>
```

```shell
cd infra

terraform init -backend-config="backend_config.hcl"
```

### Deploy Infrastructure Resources
```shell
cd infra

terraform plan -out main.tfplan
terraform apply "main.tfplan"
```

### Register Bot Webhook
```shell
curl --location 'https://api.telegram.org/bot<BOT_TOKEN>/setWebhook' \
--form 'url="<API_GATEWAY_STAGE_INVOCATION_URL>/anonymous-bot"'
```

