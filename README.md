# Anonymous Telegram Chat Bot [UNDER DEVELOPMENT]

## Deployment
### Terraform Backend
First we create a S3 bucket to store Terraform state, a DynamoDB table to persist Terraform state lock and a S3 bucket to deploy Lambda function code bundles. The Terraform state for this init stack is being kept locally.
```shell
cd infra/tf_backend
terraform init # Run once
```

Create a `terraform.tfvars` file in `infra/init`:
```hcl
aws_region = <AWS_REGION>
aws_profile = <AWS_PROFILE>
terraform_state_bucket = <S3_TERRAFORM_STATE_BUCKET_NAME>
```
**Note:** Here `<AWS_PROFILE>` and `<AWS_REGION>` are the profile (and/or account) and the region that we use to store and manage terraform state and lock.  

Now run the plan command:
```shell
terraform plan -out tf_backend.tfplan
```

After planning for the changes, apply the changeset:
```shell
terraform apply "tf_backend.tfplan"
```

### Deploy Initial Resources
Now we create and deploy the resources needed for the main stack. Go to `infra/init` directory and create a file named `backend_config.hcl`:
```hcl
profile        = <AWS_PROFILE>
bucket         = <S3_TERRAFORM_STATE_BUCKET_NAME>
region         = <AWS_REGION>
```
**Note:** Here `<AWS_PROFILE>` and `<AWS_REGION>` are the profile (and/or account) and the region that we use to store and manage terraform state and lock.

Create a file named `terraform.tfvars`:
```hcl
init_aws_region = <AWS_REGION>
init_aws_profile = {
  development = <AWS_DEV_PROFILE>
  production  = <AWS_PROD_PROFILE>
}
init_lambda_bucket = <S3_LAMBDA_CODE_BUCKET>
github_repo        = <GITHUB_PROFILE?REPO_NAME>
```
**Note:** Here `<AWS_DEV_PROFILE>`, `<AWS_PROD_PROFILE>` and `<AWS_REGION>` are the profile (and/or account) and the region that we use to deploy the main resources.

Now plan and apply the changeset:
```shell
terraform init -backend-config backend_config.hcl # Run once
terraform plan -out init.tfplan
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
aws s3 cp lambda_function.zip s3://<S3_LAMBDA_CODE_BUCKET>/lambda_function.zip --profile <AWS_PROFILE>
```
**Note:** Here `<AWS_PROFILE>` is the profile (and/or account) that we use to deploy the main resources.

### Deploy Main AWS Resources
#### Add Terraform Variables File
Create a file named `infra/terraform.tfvars`:
```hcl
aws_region      = <AWS_REGION>
aws_profile = {
    development = <AWS_DEV_PROFILE>
    production  = <AWS_PROD_PROFILE>
}
lambda_bucket   = <S3_LAMBDA_CODE_BUCKET>
bot_token       = <TELEGRAM_BOT_TOKEN>
```
**Note:** Here `<AWS_DEV_PROFILE>`, `<AWS_PROD_PROFILE>` and `<AWS_REGION>` are the profile (and/or account) and the region that we use to deploy the main resources.

#### Initialize Terraform
Create a Terraform backend configuration file named `infra/backend_config.hcl`:
```hcl
region       = <AWS_REGION>
profile      = <AWS_PROFILE>
bucket       = <S3_TERRAFORM_STATE_BUCKET_NAME>
```
**Note:** Here `<AWS_PROFILE>` and `<AWS_REGION>` are the profile (and/or account) and the region that we use to store and manage terraform state and lock.

Initialize the main Terraform stack:
```shell
cd infra
terraform init -backend-config backend_config.hcl # Run once
terraform workspace new production # Run once - Repeat it for other workspaces
terraform workspace select production
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