-- Create "usage_records" table
CREATE TABLE `usage_records` (`id` uuid NOT NULL, `created_at` datetime NOT NULL, `proxy_key_id` text NOT NULL, `proxy_key_name` text NOT NULL, `virtual_model` text NOT NULL, `provider_id` text NULL, `provider_name` text NULL, `key_id` text NULL, `key_name` text NULL, `model` text NULL, `status` integer NOT NULL, `duration_ms` integer NOT NULL, `input_tokens` integer NULL, `output_tokens` integer NULL, `cache_read_tokens` integer NULL, `cache_creation_tokens` integer NULL, `steps` integer NOT NULL, `affinity` text NULL, PRIMARY KEY (`id`));
-- Create index "usagerecord_created_at" to table: "usage_records"
CREATE INDEX `usagerecord_created_at` ON `usage_records` (`created_at`);
-- Create index "usagerecord_proxy_key_id_created_at" to table: "usage_records"
CREATE INDEX `usagerecord_proxy_key_id_created_at` ON `usage_records` (`proxy_key_id`, `created_at`);
-- Create index "usagerecord_virtual_model_created_at" to table: "usage_records"
CREATE INDEX `usagerecord_virtual_model_created_at` ON `usage_records` (`virtual_model`, `created_at`);
-- Create index "usagerecord_provider_id_created_at" to table: "usage_records"
CREATE INDEX `usagerecord_provider_id_created_at` ON `usage_records` (`provider_id`, `created_at`);
