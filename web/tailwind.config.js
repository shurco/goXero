import forms from '@tailwindcss/forms';

/** Legacy JS config — loaded from `src/app.css` via `@config` (Tailwind v4). */
/** @type {import('tailwindcss').Config} */
export default {
	content: ['./src/**/*.{html,js,svelte,ts}'],
	theme: {
		extend: {
			colors: {
				// Xero-like palette — navy-into-blue gradient on the top bar,
				// white drop-downs, dark navy for the active pill.
				brand: {
					50:  '#eef4fb',
					100: '#d7e3f4',
					200: '#aec6e8',
					300: '#7ea5d9',
					400: '#5383c6',
					500: '#2c6cb0', // top bar / primary
					600: '#23578f',
					700: '#1c4270',
					800: '#163455',
					900: '#10253d',
					950: '#0a1627'
				},
				ink: {
					50:  '#f7f8fa',
					100: '#eef0f4',
					200: '#d6dae3',
					300: '#b1b8c8',
					400: '#7f8aa1',
					500: '#5d6880',
					600: '#48526a',
					700: '#3a4258',
					800: '#2c3347',
					900: '#1c2133'
				}
			},
			fontFamily: {
				sans: [
					'Inter var',
					'Inter',
					'ui-sans-serif',
					'system-ui',
					'-apple-system',
					'Segoe UI',
					'Roboto',
					'Helvetica',
					'Arial',
					'sans-serif'
				]
			},
			boxShadow: {
				card: '0 1px 2px 0 rgba(17,24,39,.04), 0 1px 3px 0 rgba(17,24,39,.06)',
				pop:  '0 10px 30px -12px rgba(17,24,39,.22)'
			}
		}
	},
	plugins: [forms]
};
