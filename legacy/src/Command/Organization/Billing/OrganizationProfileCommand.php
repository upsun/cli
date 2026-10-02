<?php

declare(strict_types=1);

namespace Platformsh\Cli\Command\Organization\Billing;

use Platformsh\Cli\Selector\Selector;
use Platformsh\Cli\Service\Api;
use GuzzleHttp\Exception\BadResponseException;
use Platformsh\Cli\Console\AdaptiveTableCell;
use Platformsh\Cli\Console\Argument;
use Platformsh\Cli\Service\PropertyFormatter;
use Platformsh\Cli\Service\Table;
use Platformsh\Client\Model\Organization\Profile;
use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;

#[AsCommand(name: 'organization:billing:profile', description: "View or change an organization's billing profile")]
class OrganizationProfileCommand extends BillingCommandBase
{
    public function __construct(private readonly Api $api, private readonly PropertyFormatter $propertyFormatter, private readonly Selector $selector, private readonly Table $table)
    {
        parent::__construct();
    }
    protected function configure(): void
    {
        $this->selector->addOrganizationOptions($this->getDefinition(), true);
        $this->addCompleter($this->selector);
        $this->addArgument('property', InputArgument::OPTIONAL, 'The name of a property to view or change')
            ->addArgument('value', InputArgument::OPTIONAL, 'A new value for the property');
        PropertyFormatter::configureInput($this->getDefinition());
        Table::configureInput($this->getDefinition());
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $org = $this->selector->selectOrganization($input, self::BILLING_LINKS);
        $newBilling = $this->isNewBilling($org);
        if ($newBilling) {
            $profile = $this->loadBillingProfile($org);
            // The address is displayed by the org:billing:address command.
            $properties = \array_diff_key($profile->getProperties(), \array_flip(self::PROFILE_ADDRESS_PROPERTIES));
        } else {
            $profile = $org->getProfile();
            $properties = $profile->getProperties();
        }

        $property = Argument::stringOrNull($input, 'property');
        if ($property === null) {
            $headings = [];
            $values = [];
            foreach ($properties as $key => $value) {
                $headings[] = new AdaptiveTableCell($key, ['wrap' => false]);
                $values[] = $this->propertyFormatter->format($value, $key);
            }

            $table = $this->table;

            if (!$table->formatIsMachineReadable()) {
                $this->stdErr->writeln(\sprintf('Billing profile for the organization %s:', $this->api->getOrganizationLabel($org)));
            }

            $table->renderSimple($values, $headings);

            if (!$table->formatIsMachineReadable()) {
                $this->stdErr->writeln(\sprintf('To view the billing address, run: <info>%s</info>', $this->otherCommandExample($input, 'org:billing:address')));
                $this->stdErr->writeln(\sprintf('To view organization details, run: <info>%s</info>', $this->otherCommandExample($input, 'org:info')));
            }
            return 0;
        }

        $value = Argument::stringOrNull($input, 'value');
        if ($value === null) {
            $this->propertyFormatter->displayData($output, $properties, $property);
            return 0;
        }

        return $this->setProperty($property, $value, $profile, $newBilling);
    }

    protected function setProperty(string $property, string $value, Profile $profile, bool $newBilling): int
    {
        if (!$this->validateValue($property, $value, $newBilling)) {
            return 1;
        }

        $currentValue = $profile->getProperty($property, false);
        if ($currentValue === $value) {
            $this->stdErr->writeln(sprintf(
                'Property <info>%s</info> already set as: %s',
                $property,
                $this->propertyFormatter->format($profile->getProperty($property, false), $property),
            ));

            return 0;
        }
        if ($newBilling && !$profile->hasLink('update')) {
            $this->stdErr->writeln('You do not have permission to update the billing profile.');
            return 1;
        }
        try {
            $profile->update([$property => $value]);
        } catch (BadResponseException $e) {
            if ($newBilling) {
                if ($this->printBillingProfileError($e)) {
                    return 1;
                }
                throw $e;
            }
            // Translate validation error messages.
            if ($e->getResponse()->getStatusCode() === 400 && ($body = $e->getResponse()->getBody())) {
                $detail = \json_decode((string) $body, true);
                if (\is_array($detail) && !empty($detail['detail'][$property])) {
                    $this->stdErr->writeln("Invalid value for <error>$property</error>: " . $detail['detail'][$property]);
                    return 1;
                }
                if (\is_array($detail) && isset($detail['detail']) && \is_string($detail['detail'])) {
                    $this->stdErr->writeln($detail['detail']);
                    return 1;
                }
            }
            throw $e;
        }
        $this->stdErr->writeln(sprintf(
            'Property <info>%s</info> set to: %s',
            $property,
            $this->propertyFormatter->format($profile->$property, $property),
        ));

        return 0;
    }

    /**
     * Gets the type of a writable property.
     */
    private function getType(string $property, bool $newBilling): string|false
    {
        if ($newBilling) {
            $writableProperties = [
                'name' => 'string',
                'billing_email' => 'string',
                'currency' => 'string',
            ];

            return $writableProperties[$property] ?? false;
        }
        $writableProperties = [
            'company_name' => 'string',
            'billing_contact' => 'string',
            'vat_number' => 'string',
            'default_catalog' => 'string',
            'project_options_url' => 'string',
        ];

        return $writableProperties[$property] ?? false;
    }

    private function validateValue(string $property, string &$value, bool $newBilling): bool
    {
        $type = $this->getType($property, $newBilling);
        if (!$type) {
            $this->stdErr->writeln("Property not writable: <error>$property</error>");

            return false;
        }
        return \settype($value, $type);
    }
}
