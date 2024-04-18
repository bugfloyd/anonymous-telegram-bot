# Anonymous Telegram Chat Bot [UNDER DEVELOPMENT]

## Deployment
### Terraform Backend
First we create a S3 bucket to store Terraform state, a DynamoDB table to persist Terraform state lock and a S3 bucket to deploy Lambda function code bundles. The Terraform state for this init stack is being kept locally.
```shell
cd infra/init
terraform init # Run once

terraform plan \
-var aws_region=<AWS_REGION>\
-var terraform_state_bucket=<S3_TERRAFORM_STATE_BUCKET_NAME> \
-var lambda_bucket=<S3_CODE_BUCKET_NAME> \
-out init.tfplan

terraform apply "init.tfplan"
```

### Build and Bundle
```shell
cd bot

GOARCH=amd64 GOOS=linux go build -o bootstrap main.go
```
**Note:** In order to use the binary as the Lambda handler, it should be named `bootstrap`.

Create a zip bundle from the built binary
```shell
zip lambda_function.zip bootstrap
```

#### Upload Bundle
Upload the build zip bundle to S3:
```shell
aws s3 cp lambda_function.zip s3://<S3_CODE_BUCKET_NAME>/lambda_function.zip
```

### Deploy AWS Resources
#### Add Terraform Variables File
Create a file named `infra/terraform.tfvars`:
```hcl
aws_region     = <AWS_REGION>
lambda_bucket  = <S3_CODE_BUCKET_NAME>
bot_token      = <TELEGRAM_BOT_TOKEN>
```
You can also pass these variables to `terraform plan` command using multiple `-var` options.

#### Initialize Terraform
Create a Terraform backend configuration file named `infra/backend_config.hcl`:
```hcl
bucket         = <S3_TERRAFORM_STATE_BUCKET_NAME>
region         = <AWS_REGION>
```
Initialize the main Terraform stack:
```shell
cd infra

terraform init -backend-config="backend_config.hcl" # Run once
```

#### Deploy
Use `terraform plan` to create a changeset and run `terraform apply` command to deploy the changeset on AWS:
```shell
cd infra

terraform plan -out main.tfplan
terraform apply "main.tfplan"
```

### Register Bot Webhook
Get the `API_GATEWAY_STAGE_INVOCATION_URL` from terraform output and register it as the webhook on telegram.

```shell
curl https://api.telegram.org/bot<BOT_TOKEN>/setWebhook \
-F "url=<API_GATEWAY_STAGE_INVOCATION_URL>
```

## Local Development
Use docker compose to run local DynamoDB and also a local development web server on port 8080:  
```shell
export BOT_TOKEN=<BOT_TOKEN>
docker compose up -d
```

After applying a change in the bot code, to update the local bot in the docker container:
```shell
docker compose restart bot
```

Run a reverse proxy tool with a public secure API gateway on your local 8080 port.   
For example using [ngrok](https://ngrok.com/):
```shell
ngrok http 8080
```

Register the local bot on Telegram using the ngrok's forwarding endpoint:
```shell
curl https://api.telegram.org/bot<BOT_TOKEN>/setWebhook \
-F "url=<NGROK_FORWARDING_ENDPOINT>
```
You can see the incoming update requests sent by Telegram on ngrok's web interface: `http://127.0.0.1:4040`.