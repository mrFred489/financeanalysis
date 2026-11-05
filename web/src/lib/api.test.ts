import { afterEach, describe, expect, it, vi } from 'vitest';
import { APIError, apiFetch } from './api';

const originalFetch = globalThis.fetch;

afterEach(() => {
	globalThis.fetch = originalFetch;
});

describe('apiFetch error parsing', () => {
	it('throws APIError with code/message from the error shape', async () => {
		globalThis.fetch = vi.fn().mockResolvedValue(
			new Response(
				JSON.stringify({ error: { code: 'database_exists', message: 'Database already exists' } }),
				{
					status: 409,
					headers: { 'Content-Type': 'application/json' }
				}
			)
		);
		await expect(apiFetch('/database', { method: 'POST' })).rejects.toMatchObject({
			name: 'APIError',
			code: 'database_exists',
			message: 'Database already exists',
			status: 409
		});
	});

	it('falls back to a generic APIError when the body is not JSON', async () => {
		globalThis.fetch = vi.fn().mockResolvedValue(new Response('nope', { status: 500 }));
		await expect(apiFetch('/status')).rejects.toBeInstanceOf(APIError);
	});

	it('returns parsed JSON on success', async () => {
		globalThis.fetch = vi
			.fn()
			.mockResolvedValue(
				new Response(JSON.stringify({ version: 'dev', database: 'ready' }), { status: 200 })
			);
		const res = await apiFetch<{ version: string; database: string }>('/status');
		expect(res.database).toBe('ready');
	});
});
