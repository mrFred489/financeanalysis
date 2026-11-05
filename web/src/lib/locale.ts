import { writable } from 'svelte/store';

// Display locale for Intl formatting (NFR-7). Default da-DK per PRD 5.3;
// overridden from the settings table when the app loads.
export const locale = writable<string>('da-DK');
