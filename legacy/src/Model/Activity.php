<?php

declare(strict_types=1);

namespace Platformsh\Cli\Model;

use Platformsh\Client\Model\Activity as ApiActivity;

class Activity
{
    /**
     * Calculates the duration of an activity, whether complete or not.
     */
    public function getDuration(ApiActivity $activity, ?int $now = null): float|int|null
    {
        if ($activity->isComplete()) {
            $end = !empty($activity->completed_at) ? strtotime($activity->completed_at) : false;
        } elseif ($activity->state === ApiActivity::STATE_CANCELLED && $activity->hasProperty('cancelled_at')) {
            $end = strtotime((string) $activity->getProperty('cancelled_at'));
        } elseif (!empty($activity->started_at)) {
            $now = $now === null ? time() : $now;
            $end = $now;
        } else {
            $end = !empty($activity->updated_at) ? strtotime($activity->updated_at) : false;
        }
        $start = !empty($activity->started_at)
            ? strtotime($activity->started_at)
            : (!empty($activity->created_at) ? strtotime($activity->created_at) : false);

        return $end !== false && $start !== false && $end - $start > 0 ? $end - $start : null;
    }
}
