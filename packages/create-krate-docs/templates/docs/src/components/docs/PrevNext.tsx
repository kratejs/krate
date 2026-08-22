interface PrevNextProps {
  prevTitle?: string;
  prevLink?: string;
  nextTitle?: string;
  nextLink?: string;
}

export default function PrevNext({ prevTitle, prevLink, nextTitle, nextLink }: PrevNextProps) {
  const hasPrev = prevLink && prevTitle;
  const hasNext = nextLink && nextTitle;

  if (!hasPrev && !hasNext) return <span />;

  return (
    <div class="docs-nav">
      {hasPrev && (
        <a class="nav-prev" href={prevLink}>
          <span class="nav-direction">Previous</span>
          <span class="nav-title">{prevTitle}</span>
        </a>
      )}
      {hasNext && (
        <a class="nav-next" href={nextLink}>
          <span class="nav-direction">Next</span>
          <span class="nav-title">{nextTitle}</span>
        </a>
      )}
    </div>
  );
}
