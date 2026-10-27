// SPA estática (`adapter-static`): sem SSR no dispositivo; a raiz é prerenderizada em
// `index.html` e as demais navegações caem no fallback `200.html` (vite.config.ts).
export const ssr = false;
export const prerender = true;
