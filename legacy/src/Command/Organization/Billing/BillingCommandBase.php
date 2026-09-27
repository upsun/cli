<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Organization\Billing;

use GuzzleHttp\Exception\BadResponseException;
use GuzzleHttp\Utils;
use Platformsh\Cli\Command\Organization\OrganizationCommandBase;
use Platformsh\Cli\Service\Api;
use Platformsh\Client\Model\Organization\Organization;
use Platformsh\Client\Model\Organization\Profile;
use Symfony\Contracts\Service\Attribute\Required;

/**
 * Base class for commands supporting both the legacy and the new billing systems.
 */
abstract class BillingCommandBase extends OrganizationCommandBase
{
    /**
     * Links that indicate access to billing details, in the legacy or the new billing system.
     */
    protected const BILLING_LINKS = ['orders', 'billing-profile'];

    /**
     * Address properties of a billing profile on the new billing system.
     */
    protected const PROFILE_ADDRESS_PROPERTIES = ['country', 'billing_street_1', 'billing_street_2', 'locality', 'administrative_area', 'postal_code'];

    private Api $billingApi;

    #[Required]
    public function autowireBilling(Api $api): void
    {
        $this->billingApi = $api;
    }

    /**
     * Checks whether the organization uses the new billing system.
     */
    protected function isNewBilling(Organization $org): bool
    {
        return $org->getProperty('billing_legacy', false, false) === false;
    }

    /**
     * Loads the billing profile of an organization on the new billing system.
     */
    protected function loadBillingProfile(Organization $org): Profile
    {
        if (!$org->hasLink('billing-profile')) {
            if (empty($org->getProperty('billing_profile_id', false, false))) {
                throw new \RuntimeException(\sprintf('No billing profile is attached to the organization %s.', $this->billingApi->getOrganizationLabel($org, 'comment')));
            }
            throw new \RuntimeException(\sprintf('You do not have access to the billing profile of the organization %s.', $this->billingApi->getOrganizationLabel($org, 'comment')));
        }
        $url = $org->getLink('billing-profile');
        $client = $this->billingApi->getHttpClient();
        $data = Utils::jsonDecode((string) $client->request('get', $url)->getBody(), true);
        if (!\is_array($data)) {
            throw new \RuntimeException('Invalid billing profile response');
        }

        return new Profile($data, $url, $client);
    }

    /**
     * Prints a billing profile API error, if it can be translated.
     *
     * @return bool True if the error was printed, false otherwise.
     */
    protected function printBillingProfileError(BadResponseException $e): bool
    {
        $status = $e->getResponse()->getStatusCode();
        if ($status !== 400 && $status !== 403 && $status !== 409) {
            return false;
        }
        $detail = \json_decode((string) $e->getResponse()->getBody(), true);
        if (!\is_array($detail) || !isset($detail['detail']) || !\is_string($detail['detail'])) {
            return false;
        }
        $this->stdErr->writeln($detail['detail']);
        if (isset($detail['errors']) && \is_array($detail['errors'])) {
            foreach ($detail['errors'] as $field => $message) {
                if (\is_string($message)) {
                    $this->stdErr->writeln(\sprintf('  <error>%s</error>: %s', $field, $message));
                }
            }
        }
        return true;
    }
}
