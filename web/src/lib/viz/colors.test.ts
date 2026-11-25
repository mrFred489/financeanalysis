import { describe, it, expect } from 'vitest';
import { buildColorMap, categoryColor, subcategoryColor, UNCATEGORIZED_COLOR } from './colors';
import type { CategoryWithSubcategories } from '$lib/api';

const categories: CategoryWithSubcategories[] = [
	{
		id: 1,
		key: 'income',
		name: 'Income',
		color: '#00b894',
		subcategories: [
			{
				id: 10,
				category_id: 1,
				name: 'Salary',
				color: '#00b894',
				icon: null,
				hotkey: null,
				sort_order: 0
			}
		]
	},
	{
		id: 3,
		key: 'variable',
		name: 'Variable spending',
		color: '#6c5ce7',
		subcategories: [
			{
				id: 30,
				category_id: 3,
				name: 'Groceries',
				color: '#6c5ce7',
				icon: null,
				hotkey: null,
				sort_order: 0
			}
		]
	}
];

describe('buildColorMap', () => {
	const map = buildColorMap(categories);

	it('maps category keys to colors', () => {
		expect(map.category.income).toBe('#00b894');
		expect(map.category.variable).toBe('#6c5ce7');
	});

	it('maps subcategory ids to colors', () => {
		expect(map.subcategory[10]).toBe('#00b894');
		expect(map.subcategory[30]).toBe('#6c5ce7');
	});

	it('maps subcategory names and categories', () => {
		expect(map.subcategoryName[30]).toBe('Groceries');
		expect(map.subcategoryCategory[30]).toBe('variable');
	});

	it('categoryColor returns fallback for unknown', () => {
		expect(categoryColor(map, 'unknown')).toBe(UNCATEGORIZED_COLOR);
	});

	it('subcategoryColor returns fallback for unknown', () => {
		expect(subcategoryColor(map, 999)).toBe(UNCATEGORIZED_COLOR);
	});
});
