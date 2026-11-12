-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_providers_keys" table
CREATE TABLE `new_providers_keys` (`id` uuid NOT NULL, `name` text NOT NULL, `ciphertext` blob NOT NULL, `auth_mode` text NOT NULL DEFAULT ('api_key'), `token_created_at` datetime NULL, `enabled` bool NOT NULL DEFAULT (true), `max_in_flight` integer NOT NULL, `weight` integer NOT NULL DEFAULT (1), `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `provider_keys` uuid NOT NULL, PRIMARY KEY (`id`), CONSTRAINT `providers_keys_providers_providers_keys` FOREIGN KEY (`provider_keys`) REFERENCES `providers_providers` (`id`) ON DELETE CASCADE);
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
