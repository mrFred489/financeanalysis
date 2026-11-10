// Keyboard mapping for the categorize page (CATZ-8/CATZ-9, plan D7).
// Pure and testable: the Svelte page feeds KeyboardEvent fields in and performs
// the returned action.

export type CatzAction =
	| { type: 'move'; delta: number; extendSelection: boolean }
	| { type: 'assign'; subcategoryId: number }
	| { type: 'openPicker' }
	| { type: 'spread' }
	| { type: 'reset' }
	| { type: 'createRule' }
	| { type: 'undo' }
	| { type: 'redo' }
	| { type: 'focusFilter' }
	| { type: 'help' }
	| { type: 'closeOverlay' }
	| { type: 'none' };

// Keys reserved for navigation/actions (D7): they can never be subcategory
// hotkeys. Compared after normalizeKey().
export const RESERVED_KEYS: ReadonlySet<string> = new Set([
	'arrowup',
	'arrowdown',
	'k',
	'j',
	' ', // space opens the picker
	'backspace', // reset to rules
	's', // spread dialog
	'r', // create rule from line (M3, CATZ-7)
	'/', // focus filter
	'?', // help overlay
	'z', // undo/redo (with ctrl)
	'escape'
]);

export function isReservedKey(key: string): boolean {
	return RESERVED_KEYS.has(normalizeKey(key));
}

// normalizeKey maps a KeyboardEvent.key to the comparison form: lowercase,
// space for ' ' / 'Spacebar'.
export function normalizeKey(key: string): string {
	const k = key.toLowerCase();
	if (k === 'spacebar' || k === 'space') return ' ';
	return k;
}

export interface KeyMappingInput {
	key: string;
	ctrlKey?: boolean;
	metaKey?: boolean;
	shiftKey?: boolean;
	/** normalized hotkey -> subcategoryId */
	hotkeys: Map<string, number>;
	/** true while the picker/help/spread overlay is open */
	overlayOpen: boolean;
}

// mapKeyToAction maps one key event to a categorize action (D7).
export function mapKeyToAction(input: KeyMappingInput): CatzAction {
	const key = normalizeKey(input.key);
	const mod = input.ctrlKey || input.metaKey;

	// Undo/redo work even with an overlay open (CATZ-6).
	if (mod && key === 'z') {
		return input.shiftKey ? { type: 'redo' } : { type: 'undo' };
	}

	if (input.overlayOpen) {
		if (key === 'escape') return { type: 'closeOverlay' };
		return { type: 'none' };
	}

	switch (key) {
		case 'arrowup':
		case 'k':
			return { type: 'move', delta: -1, extendSelection: !!input.shiftKey };
		case 'arrowdown':
		case 'j':
			return { type: 'move', delta: 1, extendSelection: !!input.shiftKey };
		case ' ':
			return { type: 'openPicker' };
		case 's':
			return { type: 'spread' };
		case 'backspace':
			return { type: 'reset' };
		case 'r':
			return { type: 'createRule' };
		case '/':
			return { type: 'focusFilter' };
		case '?':
			return { type: 'help' };
		case 'escape':
			return { type: 'none' };
	}

	// Subcategory hotkey (single character, not reserved).
	if (key.length === 1 && !RESERVED_KEYS.has(key)) {
		const subcategoryId = input.hotkeys.get(key);
		if (subcategoryId !== undefined) {
			return { type: 'assign', subcategoryId };
		}
	}
	return { type: 'none' };
}

// --- focus movement (CATZ-2/CATZ-4) ---

// nextFocusId moves the focus by delta within the list of line ids, clamping at
// the ends. Focus is tracked by line id, not index, so refetches don't move it.
export function nextFocusId(
	lineIds: number[],
	currentId: number | null,
	delta: number
): number | null {
	if (lineIds.length === 0) return null;
	const idx = currentId === null ? -1 : lineIds.indexOf(currentId);
	if (idx === -1) return lineIds[0];
	const next = Math.min(lineIds.length - 1, Math.max(0, idx + delta));
	return lineIds[next];
}

// clampFocus returns focusId when it is in the list, otherwise the first line.
export function clampFocus(lineIds: number[], focusId: number | null): number | null {
	if (lineIds.length === 0) return null;
	if (focusId !== null && lineIds.includes(focusId)) return focusId;
	return lineIds[0];
}

// selectionRange returns the ids between anchor and focus (inclusive), in list
// order, for Shift+Up/Down multi-select (CATZ-11).
export function selectionRange(
	lineIds: number[],
	anchorId: number | null,
	focusId: number | null
): number[] {
	if (anchorId === null || focusId === null) return focusId === null ? [] : [focusId];
	const a = lineIds.indexOf(anchorId);
	const f = lineIds.indexOf(focusId);
	if (a === -1 || f === -1) return [focusId];
	const [lo, hi] = a <= f ? [a, f] : [f, a];
	return lineIds.slice(lo, hi + 1);
}

// --- type-ahead picker (CATZ-5) ---

// fuzzyMatch is a subsequence match (type-ahead style): every character of the
// query must appear in the target, in order. Case-insensitive.
export function fuzzyMatch(query: string, target: string): boolean {
	const q = query.trim().toLowerCase();
	if (q === '') return true;
	const t = target.toLowerCase();
	let i = 0;
	for (const ch of t) {
		if (ch === q[i]) i++;
		if (i >= q.length) return true;
	}
	return i >= q.length;
}

export interface PickerItem {
	id: number;
	label: string;
	/** extra text included in the fuzzy match (e.g. category name) */
	haystack?: string;
}

// filterPickerItems fuzzy-filters subcategory items by query, keeping the
// original order for equal matches.
export function filterPickerItems<T extends PickerItem>(query: string, items: T[]): T[] {
	return items.filter((it) => fuzzyMatch(query, it.haystack ?? it.label));
}
