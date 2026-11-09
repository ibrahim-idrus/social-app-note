export function homepageLinks(base: string) {
	const path = (value: string) => `${base.replace(/\/$/, '')}${value}` || '/';
	return {
		home: path('/'),
		login: path('/app/login'),
		register: path('/app/register'),
		howItWorks: '#how-it-works'
	};
}
