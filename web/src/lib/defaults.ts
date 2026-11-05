// Default subcategories seeded by the backend (migration 00002), mirrored here
// so the welcome wizard can review/edit them before the database exists (SETUP-3).
// Colors are auto-generated server-side as shades of the category base hue (CAT-5).

export interface DefaultSubcategory {
	category_key: string;
	name: string;
	sort_order: number;
}

export const DEFAULT_SUBCATEGORIES: DefaultSubcategory[] = [
	{ category_key: 'income', name: 'Salary', sort_order: 0 },
	{ category_key: 'income', name: 'Other income', sort_order: 1 },
	{ category_key: 'fixed', name: 'Housing', sort_order: 0 },
	{ category_key: 'fixed', name: 'Insurance', sort_order: 1 },
	{ category_key: 'fixed', name: 'Subscriptions', sort_order: 2 },
	{ category_key: 'variable', name: 'Groceries', sort_order: 0 },
	{ category_key: 'variable', name: 'Dining', sort_order: 1 },
	{ category_key: 'variable', name: 'Transport', sort_order: 2 },
	{ category_key: 'variable', name: 'Shopping', sort_order: 3 },
	{ category_key: 'variable', name: 'Leisure', sort_order: 4 },
	{ category_key: 'savings', name: 'Savings transfer', sort_order: 0 },
	{ category_key: 'savings', name: 'Investment', sort_order: 1 }
];

export const CATEGORY_LABELS: Record<string, string> = {
	income: 'Income',
	fixed: 'Fixed costs',
	variable: 'Variable spending',
	savings: 'Savings'
};
