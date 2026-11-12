import { describe, expect, it } from 'vitest';
import type { LiveKey } from '#src/features/usage/usage.svelte.js';
import { accountStatus, tokenInfo } from './accounts';

const DAY = 86_400_000;
const NOW = Date.parse('2026-11-12T12:00:00Z');
const iso = (ms: number) => new Date(ms).toISOString();

const key = (over = {}) => ({
	enabled: true,
	authMode: 'SETUP_TOKEN' as const,
	tokenCreatedAt: iso(NOW - 10 * DAY),
	tokenExpiresAt: iso(NOW + 355 * DAY),
	...over
});
const live = (over: Partial<LiveKey> = {}): LiveKey => ({
	providerId: 'p',
	providerName: 'P',
	keyId: 'k',
	keyName: 'K',
	enabled: true,
	maxInFlight: 2,
	inFlight: 0,
	free: 2,
	cooldownUntil: null,
	flag: '',
	...over
});
const label = (...args: Parameters<typeof accountStatus>) => accountStatus(...args).label;

describe('accountStatus', () => {
	it('derives each state', () => {
		expect(label(key({ enabled: false }), live(), NOW)).toBe('Disabled');
		expect(label(key(), live(), NOW, false)).toBe('Disabled');
		expect(label(key(), live({ flag: 'expired' }), NOW)).toBe('Expired');
		expect(label(key({ tokenExpiresAt: iso(NOW) }), live(), NOW)).toBe('Expired');
		expect(label(key(), live({ flag: 'needs_login' }), NOW)).toBe('Needs login');
		expect(label(key(), live({ cooldownUntil: iso(NOW + 1000) }), NOW)).toMatch(
			/^Cooling down until \d\d:\d\d:\d\d$/
		);
		expect(label(key(), live({ free: 0 }), NOW)).toBe('Busy');
		expect(label(key(), live(), NOW)).toBe('Ready');
		expect(label(key(), undefined, NOW)).toBe('Ready');
	});

	it('ignores a past cooldown', () => {
		expect(label(key(), live({ cooldownUntil: iso(NOW - 1000) }), NOW)).toBe('Ready');
	});

	it('applies precedence disabled > expired > needs_login > cooling > busy', () => {
		const all = live({ flag: 'expired', cooldownUntil: iso(NOW + 1000), free: 0 });
		expect(label(key({ enabled: false }), all, NOW)).toBe('Disabled');
		expect(label(key(), all, NOW)).toBe('Expired');
		expect(label(key(), { ...all, flag: 'needs_login' }, NOW)).toBe('Needs login');
		expect(label(key(), { ...all, flag: '' }, NOW)).toMatch(/^Cooling/);
		expect(label(key(), { ...all, flag: '', cooldownUntil: null }, NOW)).toBe('Busy');
	});
});

describe('tokenInfo', () => {
	it('reports age and days left', () => {
		expect(tokenInfo(key(), NOW)).toEqual({ ageDays: 10, daysLeft: 355, level: 'ok' });
	});

	it('warns at 30 days and below, not at 31, expired at 0', () => {
		const level = (ms: number) => tokenInfo(key({ tokenExpiresAt: iso(NOW + ms) }), NOW)?.level;
		expect(level(31 * DAY)).toBe('ok');
		expect(level(30 * DAY)).toBe('warning');
		expect(level(DAY)).toBe('warning');
		expect(level(0)).toBe('expired');
		expect(level(-DAY)).toBe('expired');
	});

	it('has no age or expiry for login directories', () => {
		expect(
			tokenInfo(key({ authMode: 'LOGIN_DIR', tokenCreatedAt: null, tokenExpiresAt: null }), NOW)
		).toBeNull();
	});
});
