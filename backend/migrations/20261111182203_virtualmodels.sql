-- Create "virtualmodels_models" table
CREATE TABLE `virtualmodels_models` (`id` uuid NOT NULL, `name` text NOT NULL, `kind` text NOT NULL, `strategy` text NOT NULL DEFAULT ('round_robin'), `cache_affinity` bool NOT NULL DEFAULT (true), `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, PRIMARY KEY (`id`));
-- Create index "virtualmodels_models_name_key" to table: "virtualmodels_models"
CREATE UNIQUE INDEX `virtualmodels_models_name_key` ON `virtualmodels_models` (`name`);
-- Create "virtualmodels_entries" table
CREATE TABLE `virtualmodels_entries` (`id` uuid NOT NULL, `provider_id` uuid NOT NULL, `model` text NOT NULL, `weight` integer NOT NULL DEFAULT (1), `virtual_model_tier_entries` uuid NOT NULL, PRIMARY KEY (`id`), CONSTRAINT `virtualmodels_entries_virtualmodels_tiers_entries` FOREIGN KEY (`virtual_model_tier_entries`) REFERENCES `virtualmodels_tiers` (`id`) ON DELETE CASCADE);
-- Create index "virtualmodelentry_provider_id" to table: "virtualmodels_entries"
CREATE INDEX `virtualmodelentry_provider_id` ON `virtualmodels_entries` (`provider_id`);
-- Create index "virtualmodelentry_provider_id_model_virtual_model_tier_entries" to table: "virtualmodels_entries"
CREATE UNIQUE INDEX `virtualmodelentry_provider_id_model_virtual_model_tier_entries` ON `virtualmodels_entries` (`provider_id`, `model`, `virtual_model_tier_entries`);
-- Create "virtualmodels_tiers" table
CREATE TABLE `virtualmodels_tiers` (`id` uuid NOT NULL, `priority` integer NOT NULL, `virtual_model_tiers` uuid NOT NULL, PRIMARY KEY (`id`), CONSTRAINT `virtualmodels_tiers_virtualmodels_models_tiers` FOREIGN KEY (`virtual_model_tiers`) REFERENCES `virtualmodels_models` (`id`) ON DELETE CASCADE);
-- Create index "virtualmodeltier_priority_virtual_model_tiers" to table: "virtualmodels_tiers"
CREATE UNIQUE INDEX `virtualmodeltier_priority_virtual_model_tiers` ON `virtualmodels_tiers` (`priority`, `virtual_model_tiers`);
