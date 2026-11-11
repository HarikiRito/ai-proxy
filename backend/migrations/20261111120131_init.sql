-- Create "settings" table
CREATE TABLE `settings` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `key` text NOT NULL, `value` text NOT NULL);
-- Create index "settings_key_key" to table: "settings"
CREATE UNIQUE INDEX `settings_key_key` ON `settings` (`key`);
