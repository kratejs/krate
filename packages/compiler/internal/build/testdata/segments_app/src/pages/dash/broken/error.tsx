import { useEffect } from 'react';
export default function BrokenError({ error }) {
  useEffect(() => {
    throw new Error('error page failed after: ' + error.message);
  }, []);
  return <p id="broken-error">broken error page</p>;
}
