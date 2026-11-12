-- Create "settings_settings" table
CREATE TABLE `settings_settings` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `max_body_bytes` integer NOT NULL, `request_timeout_seconds` integer NOT NULL, `queue_wait_seconds` integer NOT NULL, `default_cooldown_seconds` integer NOT NULL);
