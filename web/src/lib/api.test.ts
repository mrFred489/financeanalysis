import { afterEach, describe, expect, it, vi } from 'vitest';
import { APIError, apiFetch, listImports } from './api';

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

describe('listImports undone_at mapping', () => {
	// The Go API serializes sql.NullString as {"String":"","Valid":false} for NULL;
	// mapBatch must convert that to a real null so the UI truthiness checks work.
	it('maps an active batch to undone_at: null', async () => {
		globalThis.fetch = vi.fn().mockResolvedValue(
			new Response(
				JSON.stringify([
					{
						id: 1,
						account_id: 1,
						account_name: 'Main',
						file_name: 'a.csv',
						file_sha256: 'x',
						imported_at: '2026-11-05T10:00:00Z',
						line_count: 3,
						duplicate_count: 0,
						undone_at: { String: '', Valid: false },
						date_from: '2026-11-01',
						date_to: '2026-11-05',
						actual_line_count: 3
					}
				]),
				{ status: 200 }
			)
		);
		const [b] = await listImports();
		expect(b.undone_at).toBeNull();
	});

	it('maps an undone batch to the timestamp string', async () => {
		globalThis.fetch = vi.fn().mockResolvedValue(
			new Response(
				JSON.stringify([
					{
						id: 1,
						account_id: 1,
						account_name: 'Main',
						file_name: 'a.csv',
						file_sha256: 'x',
						imported_at: '2026-11-05T10:00:00Z',
						line_count: 3,
						duplicate_count: 0,
						undone_at: { String: '2026-11-05T11:00:00Z', Valid: true },
						date_from: '2026-11-01',
						date_to: '2026-11-05',
						actual_line_count: 3
					}
				]),
				{ status: 200 }
			)
		);
		const [b] = await listImports();
		expect(b.undone_at).toBe('2026-11-05T11:00:00Z');
	});
});
