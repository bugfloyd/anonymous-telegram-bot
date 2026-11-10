# WebSocket API, used only for pushes from the server to open apps. Every
# action goes through the HTTPS API, so a dropped socket loses nothing. The
# ws function authenticates each connection on $connect; no access or
# execution logs are kept (they would record IP addresses and connection IDs).
resource "aws_apigatewayv2_api" "ws" {
  name                       = "${local.prefix}-ws"
  description                = "Incognito Chat pushes (${var.environment})"
  protocol_type              = "WEBSOCKET"
  route_selection_expression = "$request.body.action"
}

resource "aws_apigatewayv2_integration" "ws" {
  api_id                    = aws_apigatewayv2_api.ws.id
  integration_type          = "AWS_PROXY"
  integration_uri           = aws_lambda_function.fn["ws"].invoke_arn
  content_handling_strategy = "CONVERT_TO_TEXT"
}

resource "aws_apigatewayv2_route" "ws" {
  for_each = toset(["$connect", "$disconnect"])

  api_id    = aws_apigatewayv2_api.ws.id
  route_key = each.key
  target    = "integrations/${aws_apigatewayv2_integration.ws.id}"
}

resource "aws_apigatewayv2_stage" "ws" {
  api_id      = aws_apigatewayv2_api.ws.id
  name        = "live"
  auto_deploy = true

  default_route_settings {
    throttling_rate_limit  = var.ws_throttle_rate_limit
    throttling_burst_limit = var.ws_throttle_burst_limit
  }
}

resource "aws_lambda_permission" "ws" {
  statement_id  = "WebSocketApi"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.fn["ws"].function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.ws.execution_arn}/*/*"
}
