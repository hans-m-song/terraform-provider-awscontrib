# Migrate data tables from AWSCC to awscontrib

This runbook describes a controlled migration of the data tables currently declared in [`data_tables.tf`](../../data_tables.tf) to [`data_tables_awscontrib.tf`](../../data_tables_awscontrib.tf). The latter is the target configuration; it must not be applied while the AWSCC resources still own the same remote objects. The provider supports non-default record import by stable Amazon Connect record ID; the safe path is to discover and import every record, reconcile its configuration, and only then change state ownership.

The procedure changes Terraform state addresses and provider ownership. It does not create a second copy of a table. Back up state, stop concurrent applies, and review every plan before applying it. The commands below are operator instructions; no state mutation or AWS call was performed while preparing this runbook.

## What is being consolidated

The target configuration reduces the AWSCC resource graph as follows:

| Existing AWSCC resources | awscontrib resource | Mapping |
| --- | --- | --- |
| `awscc_connect_data_table.feature_flags_fnq`, its attribute, and its DEFAULT record | `awscontrib_connect_data_table.feature_flags_fnq` | The attribute map and `DisasterEnabled = "false"` are owned by the table. |
| `awscc_connect_data_table.prompts_fnq`, three attributes, and its DEFAULT record | `awscontrib_connect_data_table.prompts_fnq` | The attribute map and `Warning` default are owned by the table. |
| `awscc_connect_data_table_record.prompts_fnq_housing` and `.prompts_fnq_traditional` | Same-named `awscontrib_connect_data_table_record` resources | Attribute IDs become attribute-name keys; every primary and record value is a string. |
| `awscc_connect_data_table.main_menu_prompts_fnq`, three attributes, and its DEFAULT record | `awscontrib_connect_data_table.main_menu_prompts_fnq` | The attribute map and both prompt defaults are owned by the table. |
| `awscc_connect_data_table_record.main_menu_prompts_fnq_housing` | Same-named `awscontrib_connect_data_table_record` resource | `Loop = "2"`; prompt values remain strings. |

The three tables and three non-default records are preserved. Seven standalone attribute resources and three DEFAULT record resources become nested table state. `data.aws_connect_instance.inst.id` is used for every new resource. The old `PhoneNumber` length validation and `Loop` minimum validation have no representation in the current awscontrib schema; see [Validation and behavior changes](#validation-and-behavior-changes).

## Preconditions and backups

1. Work in the same Terraform workspace, backend, AWS account, and region that currently manages the AWSCC resources. Stop other Terraform runs and avoid console/API edits during the cutover.
2. Confirm that the instance data source resolves to the instance ID used by the existing tables. The new provider expects an instance ID, not `data.aws_connect_instance.inst.arn`.
3. Save a protected state backup and a pre-migration plan before changing configuration:

   ```shell
   terraform state pull > migration-state-before-awscontrib.json
   terraform plan -out=migration-plan-before-awscontrib.tfplan
   terraform show migration-plan-before-awscontrib.tfplan
   ```

   Treat the state backup and plan as sensitive artifacts. Keep them outside source control and retain the backend's own version history.
4. Pin or otherwise record the awscontrib provider version selected for the migration. Do not remove the existing AWSCC provider declaration until no AWSCC resources remain in the state, and do not remove it at all if other modules still use it.

## Declare the provider

Add the provider to the root module's existing `required_providers` block. Keep the version constraint used by the deployment process rather than copying an unreviewed version:

```terraform
terraform {
  required_providers {
    awscontrib = {
      source = "hans-m-song/awscontrib"
    }
  }
}

provider "awscontrib" {
  profile = var.aws_profile
  region  = var.aws_region
}
```

Use the same AWS shared-configuration profile and region as the current provider. Do not put access keys or other credentials in Terraform configuration. Run `terraform init` and `terraform validate`, then inspect the provider lock-file diff before continuing.

## Discover the instance and table IDs

The new table importer requires `instance_id:data_table_id`. The second component is the data-table ID returned by Amazon Connect, not the Terraform resource label. Do not substitute an ARN unless the selected provider version's documentation explicitly permits it; use the `Id` field for this runbook.

1. Record the instance ID from the resolved data source or its state:

   ```shell
   terraform state show data.aws_connect_instance.inst
   ```

   Confirm the value corresponds to the account and region selected for the migration.
2. Match each exact table name to its remote ID. The AWSCC state may contain the ID or ARN:

   ```shell
   terraform state show awscc_connect_data_table.feature_flags_fnq
   terraform state show awscc_connect_data_table.prompts_fnq
   terraform state show awscc_connect_data_table.main_menu_prompts_fnq
   ```

   If the state value is ambiguous, query the service and match the exact `Name` before copying `Id`:

   ```shell
   aws connect list-data-tables \
     --instance-id "<INSTANCE_ID>" \
     --region "<AWS_REGION>" \
     --output json

   aws connect describe-data-table \
     --instance-id "<INSTANCE_ID>" \
     --data-table-id "<DATA_TABLE_ID>" \
     --region "<AWS_REGION>" \
     --output json
   ```

   Do not disable AWS CLI pagination while discovering tables. `ListDataTables` returns summary `Id`, `Arn`, and `Name` fields and may return a `NextToken`; `DescribeDataTable` verifies the selected table's full metadata. See the [ListDataTables API](https://docs.aws.amazon.com/connect/latest/APIReference/API_ListDataTables.html) and [DescribeDataTable API](https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeDataTable.html).
3. Write the three exact pairs down before importing:

   | Target address | Existing table name | Import ID |
   | --- | --- | --- |
   | `awscontrib_connect_data_table.feature_flags_fnq` | `FeatureFlags_FNQ` | `<INSTANCE_ID>:<FEATURE_FLAGS_DATA_TABLE_ID>` |
   | `awscontrib_connect_data_table.prompts_fnq` | `Prompts_FNQ` | `<INSTANCE_ID>:<PROMPTS_DATA_TABLE_ID>` |
   | `awscontrib_connect_data_table.main_menu_prompts_fnq` | `MainMenuPrompts_FNQ` | `<INSTANCE_ID>:<MAIN_MENU_PROMPTS_DATA_TABLE_ID>` |

## Import the tables and review targeted plans

Add the target file to a migration branch, but do not apply the new records yet. Import each table into its new address:

```shell
terraform import awscontrib_connect_data_table.feature_flags_fnq \
  '<INSTANCE_ID>:<FEATURE_FLAGS_DATA_TABLE_ID>'

terraform import awscontrib_connect_data_table.prompts_fnq \
  '<INSTANCE_ID>:<PROMPTS_DATA_TABLE_ID>'

terraform import awscontrib_connect_data_table.main_menu_prompts_fnq \
  '<INSTANCE_ID>:<MAIN_MENU_PROMPTS_DATA_TABLE_ID>'
```

For each imported table, run and review a targeted plan before touching the old state:

```shell
terraform plan -target=awscontrib_connect_data_table.feature_flags_fnq
terraform plan -target=awscontrib_connect_data_table.prompts_fnq
terraform plan -target=awscontrib_connect_data_table.main_menu_prompts_fnq
```

The reviewed result must not propose creating or replacing a table. Check the rendered plan and imported state for the exact table name, time zone, status, value-lock level, attribute names/types/primary flags, and explicit DEFAULT values. Stop if any attribute or value is missing, if an ID is wrong, or if validation loss would be unacceptable. A targeted plan is an inspection gate, not a substitute for the final un-targeted plan.

## State-only removal of AWSCC resources during a complete cutover

Execute this section only after every non-default record has been imported and its targeted plan has been accepted. The record importer seeds the stable identity without mutating Amazon Connect; the first refresh reconstructs the composite primary-value map and complete remote value map. The old non-default record blocks reference the old table resources, so keep the entire AWSCC graph active until all target records have been adopted and reviewed.

Follow [Discover and import non-default records](#discover-and-import-non-default-records) before executing the state-removal commands below.

Once all three table imports and all three record imports have no-op targeted plans, save another state backup. Then switch the active configuration so the old table, attribute, DEFAULT record, and non-default record blocks are no longer declared. Keep a copy of the original file outside the active module; do not delete it until the migration is accepted.

After confirming that the target table resources are imported, remove only the old table-side addresses from state:

```shell
terraform state rm \
  awscc_connect_data_table.feature_flags_fnq \
  awscc_connect_data_table_attribute.feature_flags_fnq_disaster_enabled \
  awscc_connect_data_table_record.feature_flags_fnq_default \
  awscc_connect_data_table.prompts_fnq \
  awscc_connect_data_table_attribute.prompts_fnq_phone_number \
  awscc_connect_data_table_attribute.prompts_fnq_warning \
  awscc_connect_data_table_attribute.prompts_fnq_welcome \
  awscc_connect_data_table_record.prompts_fnq_default \
  awscc_connect_data_table.main_menu_prompts_fnq \
  awscc_connect_data_table_attribute.main_menu_prompts_fnq_loop \
  awscc_connect_data_table_attribute.main_menu_prompts_fnq_prompt_disaster \
  awscc_connect_data_table_attribute.main_menu_prompts_fnq_prompt_standard \
  awscc_connect_data_table_record.main_menu_prompts_fnq_default
```

`terraform state rm` removes addresses from Terraform state only; it does not delete the remote tables, attributes, or values. Never run it while the old blocks remain active unless you intend Terraform to recreate those AWSCC resources. Confirm the exact addresses with `terraform state list` first, and save another state backup immediately afterward.

The three old non-default record addresses are intentionally separated below for readability. Their replacements must already be imported and reviewed. Remove them in the same locked state-only window, without running a plan or apply between the two commands:

```shell
terraform state rm \
  awscc_connect_data_table_record.prompts_fnq_housing \
  awscc_connect_data_table_record.prompts_fnq_traditional \
  awscc_connect_data_table_record.main_menu_prompts_fnq_housing
```

After both commands complete, confirm the six target addresses and absence of the old addresses with `terraform state list` before running any plan.

## Discover and import non-default records

The record importer accepts `instance_id:data_table_id:record_id` for one non-default record. It seeds only `instance_id`, `data_table_id`, and `record_id`; import performs no remote mutation. On the first refresh, the resource calls [`ListDataTablePrimaryValues`](https://docs.aws.amazon.com/connect/latest/APIReference/API_ListDataTablePrimaryValues.html) with the record ID, paginates until `NextToken` is empty, reconstructs the exact `primary_values` map, and then reads the complete remote record value set.

1. For each existing record, recover its exact composite primary-value map from the AWSCC configuration/state and use the table's remote ID. Discover the stable Amazon Connect record ID by listing primary values:

   ```shell
   aws connect list-data-table-primary-values \
     --instance-id "<INSTANCE_ID>" \
     --data-table-id "<DATA_TABLE_ID>" \
     --region "<AWS_REGION>" \
     --output json
   ```

   Keep AWS CLI pagination enabled. Inspect every `PrimaryValuesList` entry, match all `AttributeName`/`Value` pairs of the composite key exactly, and record its `RecordId`. Do not construct a record ID from the primary values or use an AWSCC Terraform address. `DEFAULT` is not a non-default record and is rejected by this importer. Stop if no entry matches or if more than one entry matches the exact composite key.
2. Record the discovered IDs before importing. The three target records in this migration use these import-ID shapes:

   | Target address | Existing record | Import ID |
   | --- | --- | --- |
   | `awscontrib_connect_data_table_record.prompts_fnq_housing` | `Prompts_FNQ` housing record | `<INSTANCE_ID>:<PROMPTS_DATA_TABLE_ID>:<PROMPTS_HOUSING_RECORD_ID>` |
   | `awscontrib_connect_data_table_record.prompts_fnq_traditional` | `Prompts_FNQ` traditional record | `<INSTANCE_ID>:<PROMPTS_DATA_TABLE_ID>:<PROMPTS_TRADITIONAL_RECORD_ID>` |
   | `awscontrib_connect_data_table_record.main_menu_prompts_fnq_housing` | `MainMenuPrompts_FNQ` housing record | `<INSTANCE_ID>:<MAIN_MENU_PROMPTS_DATA_TABLE_ID>:<MAIN_MENU_HOUSING_RECORD_ID>` |

3. With the target record blocks declared, import each remote record by its exact three-part ID:

   ```shell
   terraform import awscontrib_connect_data_table_record.prompts_fnq_housing \
     '<INSTANCE_ID>:<PROMPTS_DATA_TABLE_ID>:<PROMPTS_HOUSING_RECORD_ID>'

   terraform import awscontrib_connect_data_table_record.prompts_fnq_traditional \
     '<INSTANCE_ID>:<PROMPTS_DATA_TABLE_ID>:<PROMPTS_TRADITIONAL_RECORD_ID>'

   terraform import awscontrib_connect_data_table_record.main_menu_prompts_fnq_housing \
     '<INSTANCE_ID>:<MAIN_MENU_PROMPTS_DATA_TABLE_ID>:<MAIN_MENU_HOUSING_RECORD_ID>'
   ```

4. After each import, run a targeted plan and inspect the refreshed state:

   ```shell
   terraform plan -target=awscontrib_connect_data_table_record.prompts_fnq_housing
   terraform plan -target=awscontrib_connect_data_table_record.prompts_fnq_traditional
   terraform plan -target=awscontrib_connect_data_table_record.main_menu_prompts_fnq_housing
   ```

   Import adopts every remote non-primary value into Terraform state on refresh. The `values` configuration map is authoritative after adoption: a remote value omitted from configured `values` can appear in the plan as a deletion. Before applying, add every value that must be preserved to the target configuration and review any intentional deletion separately. The accepted targeted plan must retain the exact composite `primary_values`, every intended non-primary value, and the imported `record_id`; it must not create or replace the record.

5. If a record is absent, its first refresh removes it from state. If the API returns duplicate primary-value responses or an ambiguous match, stop and resolve the remote data before changing ownership. Do not use `terraform state mv` from an AWSCC record: the state shape and identity differ. Do not run two active Terraform owners against the same table or record during an interim period.

If an operator explicitly authorizes a destructive remote delete/recreate instead, use a maintenance window, export every primary key and value first, verify the export independently, delete the old remote records through their current owner, wait for deletion to be confirmed, remove the old record state addresses, and apply the new resources one table at a time. This is not adoption: it has downtime and data-loss risk, and a failed batch is not automatically rolled back. Record the authorization and backup location in the change ticket.

## Apply ordering after record imports

The safe order is:

1. Import all three tables and review one targeted plan per table.
2. Verify the table configuration includes every attribute and DEFAULT value.
3. Discover and import each existing non-default record as described in [Discover and import non-default records](#discover-and-import-non-default-records), then run a targeted plan for each record. The plan must preserve the exact composite primary values and all intended record values.
4. Only after every table and record import plan is accepted, save a fresh state backup and switch the active configuration from the complete AWSCC graph to the complete awscontrib graph.
5. In one locked state-only window, remove all old table, attribute, DEFAULT-record, and non-default-record addresses. Do not run a plan or apply between the state-removal commands.
6. Confirm the target and old address counts with `terraform state list`, then run an un-targeted plan. It must not propose table or record creation, replacement, or deletion. Save and review that plan before applying it. New records reference `awscontrib_connect_data_table.<name>.id`, so Terraform already orders them after their table. The old explicit DEFAULT-record `depends_on` edges are unnecessary because DEFAULT values are part of the table resource; no replacement dependency should be added.
Use `-target` only for the stated inspection gates. The final acceptance gate is a normal full plan with no unreviewed actions.

## Validation and behavior changes

The following differences are intentional and must be accepted by review:

| Legacy behavior or field | awscontrib behavior | Required review |
| --- | --- | --- |
| `PhoneNumber` validation `{ min_length = 12, max_length = 12 }` | Not represented by the current table schema. | Validate existing and future phone values outside this resource, or defer migration if this constraint must be provider-enforced. |
| `Loop` validation `{ minimum = 0 }` | Not represented by the current table schema. | Validate values in the calling workflow or defer migration if this constraint is mandatory. |
| Attribute resources and AWSCC attribute IDs | Attributes are keyed by their remote names inside the table resource. | Confirm every name, type, and primary flag before the first apply. |
| DEFAULT record resources with `ignore_changes` | `default_values` is an authoritative string map. | Expect managed drift reconciliation; remote default edits not present in configuration can be removed on apply. |
| Non-default record resources with `ignore_changes` | `values` is an authoritative string map. The first refresh after import adopts every remote non-primary value into state. | Populate `values` with every value to preserve; a remote value absent from configuration can be proposed for deletion on apply. Review every such plan action. |
| Numeric/boolean AWSCC record values | All awscontrib default, primary, and ordinary record map values are strings. | Confirm `false` is represented as `"false"` and `2` as `"2"`; the provider sends strings to Amazon Connect. |

The new resource does not represent the old `validation` blocks. It also does not preserve AWSCC lifecycle `ignore_changes`; this is a change from drift tolerance to authoritative reconciliation, not a syntax-only migration.

## Rollback

- Before any state change or apply, restore the original active configuration and use the pre-migration state backup or backend state version. Do not use `terraform destroy` to undo an import.
- After a state-only removal, restore the pre-migration state snapshot under the backend's normal locking and recovery procedure, restore the original configuration, and run a plan that shows no unintended actions. `terraform state push` is a recovery operation requiring operator authorization; verify the selected snapshot before pushing it.
- If any remote mutation has occurred, rollback is not atomic. First inspect the actual table, attributes, DEFAULT values, and records through the service API, then restore from the exported values or the previously owning configuration. Do not blindly apply both provider configurations or destroy the imported table.
- A destructive record delete/recreate cannot restore values automatically. The export made before deletion is the recovery source; validate it before recreating records.

## Post-migration checks

1. Run an ordinary, un-targeted `terraform plan`; it must report no changes for the migrated resources.
2. Check state ownership:

   ```shell
   terraform state list | rg 'awscontrib_connect_data_table'
   terraform state list | rg 'awscc_connect_data_table'
   ```

   A complete cutover should contain three awscontrib table addresses and three awscontrib record addresses, with no AWSCC table/attribute/record addresses. There is no supported table-only completion state for this migration; if the old graph remains, the migration is incomplete.
3. Independently inspect each table's attributes and stored values. The [ListDataTableAttributes API](https://docs.aws.amazon.com/connect/latest/APIReference/API_ListDataTableAttributes.html), [ListDataTablePrimaryValues API](https://docs.aws.amazon.com/connect/latest/APIReference/API_ListDataTablePrimaryValues.html), and [ListDataTableValues API](https://docs.aws.amazon.com/connect/latest/APIReference/API_ListDataTableValues.html) expose the remote schema, composite keys, DEFAULT values, and record values needed for comparison.
4. Exercise the feature-flag and prompt consumers that read these tables, including both phone-number records and the `Loop = "2"` record. Check that no duplicate composite keys or unexpected empty values were introduced.
5. Keep the pre-migration state and value exports until the first post-migration plan and application smoke checks have passed. Remove the old AWSCC provider declaration only when the state and all modules confirm it is unused.

## References

- [awscontrib data-table resource](../resources/connect_data_table.md)
- [awscontrib data-table record resource](../resources/connect_data_table_record.md)
- [Terraform `state rm`](https://developer.hashicorp.com/terraform/cli/commands/state/rm)
- [Terraform `import`](https://developer.hashicorp.com/terraform/cli/commands/import)
- [Amazon Connect `ListDataTables`](https://docs.aws.amazon.com/connect/latest/APIReference/API_ListDataTables.html)
- [Amazon Connect `DescribeDataTable`](https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeDataTable.html)
- [Amazon Connect `ListDataTableAttributes`](https://docs.aws.amazon.com/connect/latest/APIReference/API_ListDataTableAttributes.html)
- [Amazon Connect `ListDataTablePrimaryValues`](https://docs.aws.amazon.com/connect/latest/APIReference/API_ListDataTablePrimaryValues.html)
- [Amazon Connect `ListDataTableValues`](https://docs.aws.amazon.com/connect/latest/APIReference/API_ListDataTableValues.html)
