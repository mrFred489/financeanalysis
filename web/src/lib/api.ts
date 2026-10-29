// API client for FinanceAnalysis

const API_BASE = '/api';

export interface APIError {
	code: string;
	message: string;
}

export interface APIResponse<T> {
	data?: T;
	error?: APIError;
}

/**
 * Generic fetch wrapper that handles API errors consistently.
 */
export async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
	const url = API_BASE + path;
	const response = await fetch(url, options);

	if (!response.ok) {
		// Try to parse error from response
		try {
			const errorData = await response.json();
			if (errorData.error) {
				throw new APIError(
					errorData.error.code || 'unknown',
					errorData.error.message || response.statusText,
					response.status
				);
			}
		} catch {
			// If we can't parse the error, just throw a generic error
			throw new APIError('unknown', response.statusText, response.status);
		}
		throw new APIError('unknown', response.statusText, response.status);
	}

	return response.json() as Promise<T>;
}

/**
 * Custom error class for API errors.
 */
export class APIError extends Error {
	constructor(
		public readonly code: string,
		message: string,
		public readonly status: number
	) {
		super(message);
		this.name = 'APIError';
	}
}

// Status types
export interface StatusResponse {
	version: string;
	database: 'missing' | 'ready';
	schemaVersion?: number;
}

/**
 * Get the application status.
 */
export async function getStatus(): Promise<StatusResponse> {
	return apiFetch<StatusResponse>('/status');
}

/**
 * Create a new database.
 */
export async function createDatabase(): Promise<{ status: string }> {
	return apiFetch<{ status: string }>('/database', { method: 'POST' });
}
