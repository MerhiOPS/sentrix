/** A signal ring: the logo, drawn inline so there is no image to load. */
export function BrandMark() {
  return (
    <svg className="brand-mark" width="22" height="22" viewBox="0 0 22 22" aria-hidden>
      <circle cx="11" cy="11" r="9" fill="none" stroke="currentColor" strokeOpacity="0.35" strokeWidth="2" />
      <path d="M4 12h3l2-5 3 9 2-6 1 2h3" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
