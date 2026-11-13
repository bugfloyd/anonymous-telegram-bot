// Where each sticker image is (hashed files next to the app). They're all
// fetched together (catalog.ts, loadArt), so fetching them says nothing
// about which sticker is shown or sent. The images' license is shown in the
// sticker panel (catalog.ts, ART_LICENSE).

const files = import.meta.glob<string>('./art/*/*.webp', { query: '?url', import: 'default', eager: true });

/** Each sticker image's URL, by sticker ID ("pack/name"). */
export const ART: Readonly<Record<string, string>> = Object.fromEntries(
  Object.entries(files).map(([path, url]) => [path.slice('./art/'.length, -'.webp'.length), url]),
);
