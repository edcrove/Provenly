/** Sets the browser tab title of the current page (React 19 hoists <title> into <head>). */
export function PageTitle({ title }: { title: string }) {
  return <title>{`${title} · Provenly`}</title>
}
