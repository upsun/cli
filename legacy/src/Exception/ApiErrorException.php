<?php

declare(strict_types=1);

namespace Platformsh\Cli\Exception;

/**
 * Reports an API error response, with exit code 1 rather than the HTTP status.
 */
class ApiErrorException extends HttpException
{
    /** @var int */
    protected $code = 1;
}
