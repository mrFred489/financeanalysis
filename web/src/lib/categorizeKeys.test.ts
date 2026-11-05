import { describe, expect, it } from 'vitest';
import {
	clampFocus,
	filterPickerItems,
	fuzzyMatch,
	isReservedKey,
	mapKeyToAction,
	nextFocusId,
	normalizeKey,
	selectionRange,
	type KeyMappingInput
} from './categorizeKeys';

const hotkeys = new Map<string, number>([
	['g', 6],
	['d', 7]
]);

function input(patch: Partial<KeyMappingInput>): KeyMappingInput {
	return { key: '', hotkeys, overlayOpen: false, ...patch };
}

describe('mapKeyToAction', () => {
	it('maps arrows and vim keys to move', () => {
		expect(mapKeyToAction(input({ key: 'ArrowDown' }))).toEqual({
			type: 'move',
			delta: 1,
			extendSelection: false
		});
		expect(mapKeyToAction(input({ key: 'ArrowUp' }))).toEqual({
			type: 'move',
			delta: -1,
			extendSelection: false
		});
		expect(mapKeyToAction(input({ key: 'j' }))).toEqual({
			type: 'move',
			delta: 1,
			extendSelection: false
		});
		expect(mapKeyToAction(input({ key: 'k' }))).toEqual({
			type: 'move',
			delta: -1,
			extendSelection: false
		});
	});

	it('maps shift+arrows to extendSelection (CATZ-11)', () => {
		expect(mapKeyToAction(input({ key: 'ArrowDown', shiftKey: true }))).toEqual({
			type: 'move',
			delta: 1,
			extendSelection: true
		});
		expect(mapKeyToAction(input({ key: 'ArrowUp', shiftKey: true }))).toEqual({
			type: 'move',
			delta: -1,
			extendSelection: true
		});
	});

	it('maps subcategory hotkeys to assign', () => {
		expect(mapKeyToAction(input({ key: 'g' }))).toEqual({ type: 'assign', subcategoryId: 6 });
		expect(mapKeyToAction(input({ key: 'G' }))).toEqual({ type: 'assign', subcategoryId: 6 });
	});

	it('maps reserved action keys (D7)', () => {
		expect(mapKeyToAction(input({ key: ' ' }))).toEqual({ type: 'openPicker' });
		expect(mapKeyToAction(input({ key: 'Spacebar' }))).toEqual({ type: 'openPicker' });
		expect(mapKeyToAction(input({ key: 's' }))).toEqual({ type: 'spread' });
		expect(mapKeyToAction(input({ key: 'Backspace' }))).toEqual({ type: 'reset' });
		expect(mapKeyToAction(input({ key: '/' }))).toEqual({ type: 'focusFilter' });
		expect(mapKeyToAction(input({ key: '?' }))).toEqual({ type: 'help' });
		expect(mapKeyToAction(input({ key: 'z', ctrlKey: true }))).toEqual({ type: 'undo' });
		expect(mapKeyToAction(input({ key: 'z', ctrlKey: true, shiftKey: true }))).toEqual({
			type: 'redo'
		});
		expect(mapKeyToAction(input({ key: 'z', metaKey: true }))).toEqual({ type: 'undo' });
	});

	it('reserved keys are never subcategory hotkeys', () => {
		const reserved = new Map<string, number>([
			[' ', 1],
			['s', 2],
			['backspace', 3],
			['/', 4],
			['?', 5],
			['z', 6],
			['k', 7],
			['arrowup', 8]
		]);
		for (const key of [' ', 's', 'Backspace', '/', '?', 'z', 'k', 'ArrowUp']) {
			const action = mapKeyToAction(input({ key, hotkeys: reserved }));
			expect(action.type).not.toBe('assign');
			expect(isReservedKey(key)).toBe(true);
		}
		expect(isReservedKey('g')).toBe(false);
	});

	it('undo/redo work while an overlay is open, other keys are swallowed', () => {
		expect(mapKeyToAction(input({ key: 'z', ctrlKey: true, overlayOpen: true }))).toEqual({
			type: 'undo'
		});
		expect(mapKeyToAction(input({ key: 'Escape', overlayOpen: true }))).toEqual({
			type: 'closeOverlay'
		});
		expect(mapKeyToAction(input({ key: 'g', overlayOpen: true }))).toEqual({ type: 'none' });
		expect(mapKeyToAction(input({ key: 'ArrowDown', overlayOpen: true }))).toEqual({
			type: 'none'
		});
	});

	it('returns none for unmapped keys', () => {
		expect(mapKeyToAction(input({ key: 'x' }))).toEqual({ type: 'none' });
		expect(mapKeyToAction(input({ key: 'Enter' }))).toEqual({ type: 'none' });
	});

	it('normalizeKey lowercases and maps space', () => {
		expect(normalizeKey('A')).toBe('a');
		expect(normalizeKey(' ')).toBe(' ');
		expect(normalizeKey('Spacebar')).toBe(' ');
	});
});

describe('focus movement', () => {
	const ids = [10, 20, 30, 40];

	it('moves and clamps at the ends', () => {
		expect(nextFocusId(ids, 20, 1)).toBe(30);
		expect(nextFocusId(ids, 20, -1)).toBe(10);
		expect(nextFocusId(ids, 40, 1)).toBe(40);
		expect(nextFocusId(ids, 10, -1)).toBe(10);
	});

	it('starts at the first line when nothing is focused or focus is gone', () => {
		expect(nextFocusId(ids, null, 1)).toBe(10);
		expect(nextFocusId(ids, 999, 1)).toBe(10);
		expect(nextFocusId([], null, 1)).toBe(null);
	});

	it('clampFocus keeps a valid id and falls back to the first line', () => {
		expect(clampFocus(ids, 30)).toBe(30);
		expect(clampFocus(ids, 999)).toBe(10);
		expect(clampFocus(ids, null)).toBe(10);
		expect(clampFocus([], null)).toBe(null);
	});

	it('selectionRange returns the inclusive range in list order', () => {
		expect(selectionRange(ids, 30, 10)).toEqual([10, 20, 30]);
		expect(selectionRange(ids, 10, 30)).toEqual([10, 20, 30]);
		expect(selectionRange(ids, 20, 20)).toEqual([20]);
		expect(selectionRange(ids, null, 30)).toEqual([30]);
	});
});

describe('picker fuzzy filter (CATZ-5)', () => {
	const items = [
		{ id: 1, label: 'Groceries', haystack: 'Variable spending Groceries' },
		{ id: 2, label: 'Dining', haystack: 'Variable spending Dining' },
		{ id: 3, label: 'Savings transfer', haystack: 'Savings Savings transfer' }
	];

	it('empty query returns everything', () => {
		expect(filterPickerItems('', items)).toHaveLength(3);
	});

	it('subsequence match is case-insensitive', () => {
		expect(fuzzyMatch('grc', 'Groceries')).toBe(true);
		expect(fuzzyMatch('GRC', 'groceries')).toBe(true);
		expect(fuzzyMatch('xyz', 'Groceries')).toBe(false);
		expect(filterPickerItems('sav', items).map((i) => i.id)).toEqual([3]);
		// haystack includes the category name
		expect(filterPickerItems('vari', items).map((i) => i.id)).toEqual([1, 2]);
	});
});
