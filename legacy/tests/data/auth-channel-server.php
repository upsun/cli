<?php

/*
 * A fake auth channel for GoAuthTest.
 *
 * Usage: php auth-channel-server.php <socket> <response>...
 *
 * It answers one connection for each response: a line of JSON, or "close" to close the connection without answering.
 */

declare(strict_types=1);

$args = $_SERVER['argv'] ?? null;
if (!is_array($args) || !isset($args[1]) || !is_string($args[1])) {
    fwrite(STDERR, "Usage: php auth-channel-server.php <socket> <response>...\n");
    exit(1);
}
$server = stream_socket_server('unix://' . $args[1], $errno, $errstr);
if ($server === false) {
    fwrite(STDERR, $errstr . "\n");
    exit(1);
}
echo "ready\n";
foreach (array_slice($args, 2) as $response) {
    if (!is_string($response)) {
        exit(1);
    }
    $conn = stream_socket_accept($server, 30);
    if ($conn === false) {
        exit(1);
    }
    fgets($conn);
    if ($response !== 'close') {
        fwrite($conn, $response . "\n");
    }
    fclose($conn);
}
