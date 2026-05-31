import type { PollView, LibraryItem, VotingMethod, ResultsView, CreatePollBody } from './types';

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
	genres: (scope: string) =>
		req<{ genres: string[] }>('GET', `/api/genres?scope=${encodeURIComponent(scope)}`),
	createPoll: (body: CreatePollBody) => req<PollView>('POST', '/api/polls', body),
	getPoll: (code: string) => req<PollView>('GET', `/api/polls/${code}`),
	join: (code: string, display_name: string) =>
		req<PollView>('POST', `/api/polls/${code}/join`, { display_name }),
	library: (code: string, q: string, type: string) =>
		req<{ items: LibraryItem[]; total: number }>(
			'GET',
			`/api/polls/${code}/library?q=${encodeURIComponent(q)}&type=${encodeURIComponent(type)}`
		),
	nominate: (code: string, item_id: string) =>
		req<PollView>('POST', `/api/polls/${code}/nominations`, { item_id }),
	withdraw: (code: string, id: string) =>
		req<PollView>('DELETE', `/api/polls/${code}/nominations/${id}`),
	advance: (code: string) => req<PollView>('POST', `/api/polls/${code}/advance`),
	vote: (code: string, selections: Record<string, number>) =>
		req<PollView>('POST', `/api/polls/${code}/votes`, { selections }),
	results: (code: string) => req<ResultsView>('GET', `/api/polls/${code}/results`),
	imageURL: (itemId: string, tag: string) =>
		`/api/items/${itemId}/image?fillHeight=450&quality=90${tag ? `&tag=${encodeURIComponent(tag)}` : ''}`,
	events: (code: string) => new EventSource(`/api/polls/${code}/events`)
};
