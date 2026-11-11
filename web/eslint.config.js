import path from 'node:path';
import { defineConfig, includeIgnoreFile } from 'eslint/config';
import prettier from 'eslint-config-prettier';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';
import ts from 'typescript-eslint';

const gitignorePath = path.resolve(import.meta.dirname, '.gitignore');

export default defineConfig(
	includeIgnoreFile(gitignorePath),
	// Global ignores
	{
		ignores: [
			'.svelte-kit/**',
			'build/**',
			'dist/**',
			'coverage/**',
			'.ai/**',
			// shadcn-generated primitives - keep App*.svelte wrappers linted
			'src/components/ui/**/!(App)*.svelte',
			'src/components/ui/**/index.ts'
		]
	},
	// Type-aware configs for .ts files
	{
		files: ['**/*.ts'],
		extends: [...ts.configs.strictTypeChecked, ...ts.configs.stylisticTypeChecked],
		languageOptions: {
			parserOptions: {
				projectService: true
			}
		}
	},
	// Type-aware configs for .svelte files
	{
		files: ['**/*.svelte', '**/*.svelte.ts', '**/*.svelte.js'],
		extends: [...ts.configs.strictTypeChecked, ...ts.configs.stylisticTypeChecked],
		languageOptions: {
			parserOptions: {
				projectService: true,
				extraFileExtensions: ['.svelte'],
				parser: ts.parser
			}
		}
	},
	// Svelte plugin
	...svelte.configs.recommended,
	// Shared rules for all linted files
	{
		languageOptions: { globals: { ...globals.browser, ...globals.node } },
		rules: {
			'no-undef': 'off',
			// SPA mode: standard <a> links are fine; resolve() is not needed
			'svelte/no-navigation-without-resolve': 'off',
			// Strict TypeScript rules
			'@typescript-eslint/no-explicit-any': 'error',
			'@typescript-eslint/no-unused-vars': [
				'error',
				{
					vars: 'all',
					args: 'after-used',
					ignoreRestSiblings: false,
					argsIgnorePattern: '^_',
					varsIgnorePattern: '^_',
					destructuredArrayIgnorePattern: '^_',
					caughtErrorsIgnorePattern: '^(_|ignore)'
				}
			],
			'@typescript-eslint/no-floating-promises': 'error',
			'@typescript-eslint/no-misused-promises': 'error',
			'@typescript-eslint/await-thenable': 'error',
			'@typescript-eslint/no-unnecessary-type-assertion': 'error',
			'@typescript-eslint/switch-exhaustiveness-check': 'error',
			'@typescript-eslint/consistent-type-imports': ['error', { prefer: 'type-imports' }],
			'@typescript-eslint/no-non-null-assertion': 'warn',
			'@typescript-eslint/no-deprecated': 'warn',
			'prefer-const': 'error',
			'prefer-template': 'error',
			// Turn off noisy rules
			'@typescript-eslint/strict-boolean-expressions': 'off',
			'@typescript-eslint/no-unnecessary-condition': 'off',
			'@typescript-eslint/no-unnecessary-type-parameters': 'off',
			'@typescript-eslint/no-misused-spread': 'off',
			'@typescript-eslint/unbound-method': 'off',
			'@typescript-eslint/require-await': 'off',
			'@typescript-eslint/no-unsafe-argument': 'off',
			'@typescript-eslint/no-unsafe-assignment': 'off',
			'@typescript-eslint/no-unsafe-member-access': 'off',
			'@typescript-eslint/no-unsafe-call': 'off',
			'@typescript-eslint/restrict-template-expressions': 'off',
			'@typescript-eslint/prefer-nullish-coalescing': 'off',
			'@typescript-eslint/no-confusing-void-expression': 'off',
			'@typescript-eslint/no-extraneous-class': 'off',
			'no-constant-binary-expression': 'off'
		}
	},
	// Svelte 5 rune override: $bindable() in $props() requires `let`, not `const`
	// prefer-const is a false positive in Svelte components with bindable props
	{
		files: ['**/*.svelte'],
		rules: {
			'prefer-const': 'off'
		}
	},
	// Prettier last (disables formatting rules)
	prettier,
	...svelte.configs.prettier
);
