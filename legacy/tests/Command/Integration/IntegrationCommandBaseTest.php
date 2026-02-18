<?php

declare(strict_types=1);

namespace Platformsh\Cli\Tests\Command\Integration;

use PHPUnit\Framework\Attributes\Group;
use PHPUnit\Framework\TestCase;
use Platformsh\Cli\Command\Integration\IntegrationAddCommand;
use Platformsh\ConsoleForm\Field\ArrayField;

#[Group('commands')]
class IntegrationCommandBaseTest extends TestCase
{
    /** @var array<string, \Platformsh\ConsoleForm\Field\Field> */
    private array $fields;

    protected function setUp(): void
    {
        $command = new IntegrationAddCommand(
            $this->createMock(\Platformsh\Cli\Service\ActivityMonitor::class),
            $this->createMock(\Platformsh\Cli\Service\QuestionHelper::class),
            $this->createMock(\Platformsh\Cli\Selector\Selector::class),
        );
        $ref = new \ReflectionMethod($command, 'getFields');
        $ref->setAccessible(true);
        $this->fields = $ref->invoke($command);
    }

    public function testExcludedServicesFieldExists(): void
    {
        $this->assertArrayHasKey('excluded_services', $this->fields);
    }

    public function testExcludedServicesFieldIsArrayField(): void
    {
        $this->assertInstanceOf(ArrayField::class, $this->fields['excluded_services']);
    }

    public function testExcludedServicesFieldOptionName(): void
    {
        $this->assertSame('excluded-services', $this->fields['excluded_services']->getOptionName());
    }

    public function testExcludedServicesFieldNotRequired(): void
    {
        $this->assertFalse($this->fields['excluded_services']->isRequired());
    }

    public function testExcludedServicesFieldConditions(): void
    {
        $conditions = $this->fields['excluded_services']->getConditions();
        $this->assertArrayHasKey('type', $conditions);
        $expected = ['httplog', 'newrelic', 'splunk', 'sumologic', 'syslog', 'otlplog'];
        $this->assertSame($expected, $conditions['type']);
    }

    public function testExcludedServicesFieldIncludedForLogForwardingTypes(): void
    {
        $typeField = $this->fields['type'];
        $conditionValues = $this->fields['excluded_services']->getConditions()['type'];
        $logTypes = ['httplog', 'newrelic', 'splunk', 'sumologic', 'syslog', 'otlplog'];
        foreach ($logTypes as $type) {
            $this->assertTrue(
                $typeField->matchesCondition($type, $conditionValues),
                "Expected excluded_services to be included for type '$type'",
            );
        }
    }

    public function testExcludedServicesFieldExcludedForNonLogForwardingTypes(): void
    {
        $typeField = $this->fields['type'];
        $conditionValues = $this->fields['excluded_services']->getConditions()['type'];
        $nonLogTypes = [
            'bitbucket',
            'bitbucket_server',
            'github',
            'gitlab',
            'webhook',
            'health.email',
            'health.pagerduty',
            'health.slack',
            'health.webhook',
            'script',
        ];
        foreach ($nonLogTypes as $type) {
            $this->assertFalse(
                $typeField->matchesCondition($type, $conditionValues),
                "Expected excluded_services to be excluded for type '$type'",
            );
        }
    }
}
