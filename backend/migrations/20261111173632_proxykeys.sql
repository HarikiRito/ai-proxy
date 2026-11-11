-- Create "proxykeys_keys" table
CREATE TABLE `proxykeys_keys` (`id` uuid NOT NULL, `name` text NOT NULL, `key_hash` text NOT NULL, `allowed_models` json NOT NULL, `created_at` datetime NOT NULL, `last_used_at` datetime NULL, `revoked_at` datetime NULL, PRIMARY KEY (`id`));
-- Create index "proxykeys_keys_name_key" to table: "proxykeys_keys"
CREATE UNIQUE INDEX `proxykeys_keys_name_key` ON `proxykeys_keys` (`name`);
-- Create index "proxykeys_keys_key_hash_key" to table: "proxykeys_keys"
CREATE UNIQUE INDEX `proxykeys_keys_key_hash_key` ON `proxykeys_keys` (`key_hash`);
