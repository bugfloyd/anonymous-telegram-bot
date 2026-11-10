# All outputs are sensitive so Terraform never prints them: workflow logs are
# public. CI reads them with `terraform output -raw` and masks them.
output "api_url" {
  description = "HTTPS API base URL (built into the client)"
  value       = aws_lambda_function_url.api.function_url
  sensitive   = true
}

output "ws_url" {
  description = "WebSocket URL (built into the client)"
  value       = "${aws_apigatewayv2_api.ws.api_endpoint}/${aws_apigatewayv2_stage.ws.name}"
  sensitive   = true
}

output "webhook_url" {
  description = "Telegram webhook URL"
  value       = aws_lambda_function_url.bot.function_url
  sensitive   = true
}
