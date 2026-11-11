-- Drop the placeholder "settings" table
DROP TABLE `settings`;
-- Create "providers_providers" table
CREATE TABLE `providers_providers` (`id` uuid NOT NULL, `name` text NOT NULL, `kind` text NOT NULL, `base_url` text NOT NULL, `enabled` bool NOT NULL DEFAULT (true), `rotation` text NOT NULL DEFAULT ('round_robin'), `cache_affinity_seconds` integer NOT NULL DEFAULT (600), `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, PRIMARY KEY (`id`));
-- Create index "providers_providers_name_key" to table: "providers_providers"
CREATE UNIQUE INDEX `providers_providers_name_key` ON `providers_providers` (`name`);
-- Create "providers_keys" table
CREATE TABLE `providers_keys` (`id` uuid NOT NULL, `name` text NOT NULL, `ciphertext` blob NOT NULL, `last4` text NOT NULL DEFAULT (''), `enabled` bool NOT NULL DEFAULT (true), `max_in_flight` integer NOT NULL, `weight` integer NOT NULL DEFAULT (1), `priority` integer NOT NULL DEFAULT (0), `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `provider_keys` uuid NOT NULL, PRIMARY KEY (`id`), CONSTRAINT `providers_keys_providers_providers_keys` FOREIGN KEY (`provider_keys`) REFERENCES `providers_providers` (`id`) ON DELETE CASCADE);
-- Create index "providerkey_name_provider_keys" to table: "providers_keys"
CREATE UNIQUE INDEX `providerkey_name_provider_keys` ON `providers_keys` (`name`, `provider_keys`);
