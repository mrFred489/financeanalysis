// Color mapping for viz charts (M5 VIZ-6, D6): category/subcategory colors
// from the API, with fallbacks for uncategorized (neutral grey), conflict
// (warning amber) and transfer (grey).

import type { CategoryWithSubcategories, Subcategory } from '$lib/api';

export const UNCATEGORIZED_COLOR = '#9ca3af'; // neutral grey
export const CONFLICT_COLOR = '#f59e0b'; // amber
export const TRANSFER_COLOR = '#6b7280'; // grey

export interface ColorMap {
	category: Record<string, string>; // category key → color
	subcategory: Record<number, string>; // subcategory id → color
	subcategoryName: Record<number, string>; // subcategory id → name
	subcategoryCategory: Record<number, string>; // subcategory id → category key
}

export function buildColorMap(categories: CategoryWithSubcategories[]): ColorMap {
	const map: ColorMap = {
		category: {},
		subcategory: {},
		subcategoryName: {},
		subcategoryCategory: {}
	};
	for (const cat of categories) {
		map.category[cat.key] = cat.color;
		for (const sub of cat.subcategories) {
			map.subcategory[sub.id] = sub.color;
			map.subcategoryName[sub.id] = sub.name;
			map.subcategoryCategory[sub.id] = cat.key;
		}
	}
	return map;
}

export function subcategoryColor(map: ColorMap, id: number): string {
	return map.subcategory[id] ?? UNCATEGORIZED_COLOR;
}

export function categoryColor(map: ColorMap, key: string): string {
	return map.category[key] ?? UNCATEGORIZED_COLOR;
}

export function allSubcategories(categories: CategoryWithSubcategories[]): Subcategory[] {
	return categories.flatMap((c) => c.subcategories);
}
