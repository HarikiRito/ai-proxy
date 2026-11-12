import { SvelteDate } from 'svelte/reactivity';
import type { BadgeVariant, LiveKey } from '#src/features/usage/usage.svelte.js';
import type { ProviderKey } from './providers.svelte';

const DAY_MS = 86_400_000;
export const EXPIRY_WARNING_DAYS = 30;

type AccountKey = Pick<ProviderKey, 'enabled' | 'authMode' | 'tokenCreatedAt' | 'tokenExpiresAt'>;

export function accountStatus(
	key: AccountKey,
	live: LiveKey | undefined,
	now: number,
	providerEnabled = true
): { label: string; variant: BadgeVariant } {
	if (!key.enabled || !providerEnabled) return { label: 'Disabled', variant: 'secondary' };
	const expired = key.tokenExpiresAt !== null && Date.parse(key.tokenExpiresAt) <= now;
	if (live?.flag === 'expired' || expired) return { label: 'Expired', variant: 'destructive' };
	if (live?.flag === 'needs_login') return { label: 'Needs login', variant: 'destructive' };
	if (live?.cooldownUntil && Date.parse(live.cooldownUntil) > now) {
		const until = new SvelteDate(live.cooldownUntil).toLocaleTimeString([], { hour12: false });
		return { label: `Cooling down until ${until}`, variant: 'destructive' };
	}
	return live?.free === 0
		? { label: 'Busy', variant: 'outline' }
		: { label: 'Ready', variant: 'default' };
}

export interface TokenInfo {
	ageDays: number;
	daysLeft: number;
	level: 'ok' | 'warning' | 'expired';
}

// Setup tokens only; login directories have no age or expiry.
export function tokenInfo(key: AccountKey, now: number): TokenInfo | null {
	if (key.authMode !== 'SETUP_TOKEN' || !key.tokenCreatedAt || !key.tokenExpiresAt) return null;
	const daysLeft = Math.ceil((Date.parse(key.tokenExpiresAt) - now) / DAY_MS);
	return {
		ageDays: Math.floor((now - Date.parse(key.tokenCreatedAt)) / DAY_MS),
		daysLeft,
		level: daysLeft <= 0 ? 'expired' : daysLeft <= EXPIRY_WARNING_DAYS ? 'warning' : 'ok'
	};
}
