-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_providers_providers" table
CREATE TABLE `new_providers_providers` (`id` uuid NOT NULL, `name` text NOT NULL, `kind` text NOT NULL, `base_url` text NOT NULL, `enabled` bool NOT NULL DEFAULT (true), `models` json NULL, `cache_affinity_seconds` integer NOT NULL DEFAULT (600), `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, PRIMARY KEY (`id`));
-- Copy rows from old table "providers_providers" to new temporary table "new_providers_providers"
INSERT INTO `new_providers_providers` (`id`, `name`, `kind`, `base_url`, `enabled`, `cache_affinity_seconds`, `created_at`, `updated_at`) SELECT `id`, `name`, `kind`, `base_url`, `enabled`, `cache_affinity_seconds`, `created_at`, `updated_at` FROM `providers_providers`;
-- Drop "providers_providers" table after copying rows
DROP TABLE `providers_providers`;
-- Rename temporary table "new_providers_providers" to "providers_providers"
ALTER TABLE `new_providers_providers` RENAME TO `providers_providers`;
-- Create index "providers_providers_name_key" to table: "providers_providers"
CREATE UNIQUE INDEX `providers_providers_name_key` ON `providers_providers` (`name`);
-- Create "new_providers_keys" table
CREATE TABLE `new_providers_keys` (`id` uuid NOT NULL, `name` text NOT NULL, `ciphertext` blob NOT NULL, `enabled` bool NOT NULL DEFAULT (true), `max_in_flight` integer NOT NULL, `weight` integer NOT NULL DEFAULT (1), `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `provider_keys` uuid NOT NULL, PRIMARY KEY (`id`), CONSTRAINT `providers_keys_providers_providers_keys` FOREIGN KEY (`provider_keys`) REFERENCES `providers_providers` (`id`) ON DELETE CASCADE);
-- Copy rows from old table "providers_keys" to new temporary table "new_providers_keys"
INSERT INTO `new_providers_keys` (`id`, `name`, `ciphertext`, `enabled`, `max_in_flight`, `weight`, `created_at`, `updated_at`, `provider_keys`) SELECT `id`, `name`, `ciphertext`, `enabled`, `max_in_flight`, `weight`, `created_at`, `updated_at`, `provider_keys` FROM `providers_keys`;
-- Drop "providers_keys" table after copying rows
DROP TABLE `providers_keys`;
-- Rename temporary table "new_providers_keys" to "providers_keys"
ALTER TABLE `new_providers_keys` RENAME TO `providers_keys`;
-- Create index "providerkey_name_provider_keys" to table: "providers_keys"
CREATE UNIQUE INDEX `providerkey_name_provider_keys` ON `providers_keys` (`name`, `provider_keys`);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
