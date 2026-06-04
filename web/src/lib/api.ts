import type {
	PollView,
	LibraryItem,
	VotingMethod,
	ResultsView,
	CreatePollBody,
	ExternalResult,
	AdminSession,
	AdminPollSummary
} from './types';

async function req<T>(method: string, url: string, body?: unknown): Promise<T> {
	const res = await fetch(url, {
		method,
		headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
		body: body !== undefined ? JSON.stringify(body) : undefined,
		credentials: 'same-origin'
	});
	const text = await res.text();
	const data = text ? JSON.parse(text) : null;
	if (!res.ok) {
		throw new Error((data && data.error) || res.statusText || 'request failed');
	}
	return data as T;
}

export const api = {
	methods: () => req<VotingMethod[]>('GET', '/api/methods'),
	features: () => req<{ seerr: boolean; admin: boolean }>('GET', '/api/features'),
	genres: (scope: string) =>
		req<{ genres: string[] }>('GET', `/api/genres?scope=${encodeURIComponent(scope)}`),
	searchExternal: (code: string, q: string) =>
		req<{ results: ExternalResult[] }>(
			'GET',
			`/api/polls/${code}/search-external?q=${encodeURIComponent(q)}`
		),
	nominateExternal: (code: string, tmdb_id: number, media_type: string) =>
		req<PollView>('POST', `/api/polls/${code}/nominations`, { tmdb_id, media_type }),
	requestWinner: (code: string, nominationId: string) =>
		req<PollView>('POST', `/api/polls/${code}/request/${nominationId}`),
	createPoll: (body: CreatePollBody) => req<PollView>('POST', '/api/polls', body),
	getPoll: (code: string) => req<PollView>('GET', `/api/polls/${code}`),
	join: (code: string, display_name: string) =>
		req<PollView>('POST', `/api/polls/${code}/join`, { display_name }),
	library: (code: string, q: string, type: string, genre = '') =>
		req<{ items: LibraryItem[]; total: number }>(
			'GET',
			`/api/polls/${code}/library?q=${encodeURIComponent(q)}&type=${encodeURIComponent(type)}${
				genre ? `&genre=${encodeURIComponent(genre)}` : ''
			}`
		),
	nominate: (code: string, item_id: string) =>
		req<PollView>('POST', `/api/polls/${code}/nominations`, { item_id }),
	withdraw: (code: string, id: string) =>
		req<PollView>('DELETE', `/api/polls/${code}/nominations/${id}`),
	advance: (code: string) => req<PollView>('POST', `/api/polls/${code}/advance`),
	startTimer: (code: string) => req<PollView>('POST', `/api/polls/${code}/timer/start`),
	pauseTimer: (code: string) => req<PollView>('POST', `/api/polls/${code}/timer/pause`),
	extendTimer: (code: string, add_seconds: number) =>
		req<PollView>('POST', `/api/polls/${code}/timer/extend`, { add_seconds }),
	vote: (code: string, selections: Record<string, number>) =>
		req<PollView>('POST', `/api/polls/${code}/votes`, { selections }),
	results: (code: string) => req<ResultsView>('GET', `/api/polls/${code}/results`),
	imageURL: (itemId: string, tag: string) =>
		`/api/items/${itemId}/image?fillHeight=450&quality=90${tag ? `&tag=${encodeURIComponent(tag)}` : ''}`,
	events: (code: string) => new EventSource(`/api/polls/${code}/events`),

	// Admin dashboard. adminSession throws (404) when the dashboard is disabled.
	adminSession: () => req<AdminSession>('GET', '/api/admin/session'),
	adminLogin: (token: string) => req<AdminSession>('POST', '/api/admin/login', { token }),
	adminLogout: () => req<AdminSession>('POST', '/api/admin/logout'),
	adminPolls: () => req<AdminPollSummary[]>('GET', '/api/admin/polls'),
	adminPoll: (code: string) => req<PollView>('GET', `/api/admin/polls/${code}`),
	adminDeletePoll: (code: string) => req<null>('DELETE', `/api/admin/polls/${code}`),
	adminAdvance: (code: string) => req<AdminPollSummary>('POST', `/api/admin/polls/${code}/advance`)
};
