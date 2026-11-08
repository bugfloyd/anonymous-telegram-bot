output "webhook_url" {
  description = "URL to register as the Telegram webhook"
  value       = "${aws_apigatewayv2_stage.default.invoke_url}${trimprefix(local.webhook_path, "/")}"
  sensitive   = true
}

output "function_name" {
  description = "Lambda function name"
  value       = aws_lambda_function.bot.function_name
}
