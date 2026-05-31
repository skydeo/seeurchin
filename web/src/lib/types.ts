export type Status = 'draft' | 'round1' | 'round2' | 'closed';

export interface SubmissionRules {
	min: number;
	max: number;
	required: number;
}

export interface MeView {
	id: string;
	display_name: string;
	is_host: boolean;
	nomination_count: number;
	has_voted: boolean;
	my_selections: Record<string, number> | null;
}

export interface NominationView {
	id: string;
	item_id: string;
	title: string;
	year: number;
	type: string;
	runtime_minutes: number;
	overview: string;
	image_tag: string;
	nominator_count: number;
	mine_nominated: boolean;
}

export interface ResultEntry {
	nomination_id: string;
	title: string;
	score: number;
	nominators?: string[];
}

export interface RoundResult {
	round: number;
	counts: Record<string, number>;
	eliminated?: string[];
}

export interface ResultsView {
	method: string;
	winners: ResultEntry[];
	ranked: ResultEntry[];
	rounds?: RoundResult[];
}

export interface PollView {
	code: string;
	title: string;
	status: Status;
	library_scope: string;
	submission_rules: SubmissionRules;
	min_to_submit: number;
	voting_method: string;
	voting_method_label: string;
	voting_config: Record<string, unknown>;
	allow_guests: boolean;
	results_live: boolean;
	reveal_nominators: boolean;
	reveal_scope: string;
	participant_count: number;
	voter_count: number;
	nominations: NominationView[];
	me: MeView | null;
	results?: ResultsView;
	share_url: string;
}

export interface LibraryItem {
	id: string;
	title: string;
	year: number;
	type: string;
	runtime_minutes: number;
	image_tag: string;
	nominated: boolean;
}

export interface VotingMethod {
	key: string;
	label: string;
	default_config: Record<string, unknown>;
}

export interface CreatePollBody {
	title: string;
	host_name: string;
	library_scope: string;
	voting_method: string;
	voting_config: Record<string, unknown>;
	submission_rules: SubmissionRules;
	allow_guests: boolean;
	results_live: boolean;
	reveal_nominators: boolean;
	reveal_scope: string;
}
