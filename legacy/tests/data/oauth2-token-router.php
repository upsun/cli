<?php

// A mock OAuth2 token endpoint, for PHP's built-in server. It accepts only the refresh token "refresh-1".

\parse_str((string) \file_get_contents('php://input'), $params);
\header('Content-Type: application/json');
if (($params['grant_type'] ?? '') !== 'refresh_token' || ($params['refresh_token'] ?? '') !== 'refresh-1') {
    \http_response_code(400);
    echo \json_encode(['error' => 'invalid_grant', 'error_description' => 'The refresh token is invalid.']);
    return;
}
echo \json_encode([
    'access_token' => 'access-2',
    'refresh_token' => 'refresh-2',
    'expires_in' => 900,
    'token_type' => 'bearer',
]);
