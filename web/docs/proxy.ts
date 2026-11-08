import { NextRequest, NextResponse } from 'next/server';
import { isMarkdownPreferred, rewritePath } from 'fumadocs-core/negotiation';
import { docsContentRoute } from '@/lib/shared';

const { rewrite: rewriteDocs } = rewritePath('{/*path}', `${docsContentRoute}{/*path}/content.md`);
const { rewrite: rewriteSuffix } = rewritePath('{/*path}.md', `${docsContentRoute}{/*path}/content.md`);

// Only pages have a Markdown version; assets, search, images and the Markdown routes themselves pass straight through.
const passThrough = /^\/(_next\/|api\/|og\/|llms|sitemap\.xml|robots\.txt|icon|apple-icon)/;

export default function proxy(request: NextRequest) {
  if (passThrough.test(request.nextUrl.pathname)) return NextResponse.next();
  const result = rewriteSuffix(request.nextUrl.pathname);
  if (result) {
    return NextResponse.rewrite(new URL(result, request.nextUrl));
  }

  if (isMarkdownPreferred(request)) {
    const result = rewriteDocs(request.nextUrl.pathname);

    if (result) {
      return NextResponse.rewrite(new URL(result, request.nextUrl), {
        // this URL has two representations, selected by `Accept`
        headers: { Vary: 'Accept' },
      });
    }
  }

  return NextResponse.next();
}
