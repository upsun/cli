<?php

// A router for the PHP built-in web server, used by CurlStreamTest.
$uri = $_SERVER['REQUEST_URI'] ?? '';
switch (is_string($uri) ? parse_url($uri, PHP_URL_PATH) : null) {
    case '/stream':
        foreach (['one', 'two', 'three'] as $line) {
            echo $line, "\n";
            flush();
            usleep(100000);
        }
        break;
    case '/headers':
        $authorization = $_SERVER['HTTP_AUTHORIZATION'] ?? '';
        echo is_string($authorization) ? $authorization : '';
        break;
    case '/slow':
        sleep(3);
        break;
    default:
        http_response_code(404);
        echo 'Not found';
}
