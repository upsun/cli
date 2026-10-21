<?php

// A router for the PHP built-in server, used by HTTP client tests.
$uri = $_SERVER['REQUEST_URI'] ?? '';
switch (is_string($uri) ? parse_url($uri, PHP_URL_PATH) : null) {
    case '/cookie':
        $cookie = $_SERVER['HTTP_COOKIE'] ?? '';
        echo is_string($cookie) ? $cookie : '';
        break;
    default:
        http_response_code(404);
}
