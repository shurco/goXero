import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		port: 5173,
		proxy: {
			'/api':    { target: 'http://localhost:8080', changeOrigin: true },
			'/health': { target: 'http://localhost:8080', changeOrigin: true }
		}
	}
});
