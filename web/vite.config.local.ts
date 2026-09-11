import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	server: {
		port: 5174,
		proxy: {
			'/api':    { target: 'http://localhost:18080', changeOrigin: true },
			'/health': { target: 'http://localhost:18080', changeOrigin: true }
		}
	}
});
