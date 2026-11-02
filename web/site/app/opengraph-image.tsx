import { ImageResponse } from 'next/og'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'

export const alt = 'Skiff — Ship it to a server you own'
export const size = { width: 1200, height: 630 }
export const contentType = 'image/png'

export default async function Image() {
  const [regular, display] = await Promise.all([
    readFile(join(process.cwd(), 'assets/InstrumentSans-400.woff')),
    readFile(join(process.cwd(), 'assets/Bricolage-700.woff')),
  ])

  return new ImageResponse(
    (
      <div
        style={{
          width: '100%',
          height: '100%',
          display: 'flex',
          flexDirection: 'column',
          background: '#142c29',
          color: '#f0f4f1',
          fontFamily: 'Instrument Sans',
        }}
      >
        <div
          style={{
            flex: 1,
            display: 'flex',
            flexDirection: 'column',
            justifyContent: 'space-between',
            padding: '72px 84px 56px',
          }}
        >
          <svg width="84" height="84" viewBox="0 0 32 32">
            <path
              fill="#f0f4f1"
              fillRule="evenodd"
              d="M16 1.5C20.5 5 25.5 12 25.5 20C25.5 24 24.7 27.6 23.6 30.5H8.4C7.3 27.6 6.5 24 6.5 20C6.5 12 11.5 5 16 1.5ZM11.1 13.2H20.9Q21.5 13.2 21.5 13.8V15.2Q21.5 15.8 20.9 15.8H11.1Q10.5 15.8 10.5 15.2V13.8Q10.5 13.2 11.1 13.2ZM11.1 18.4H20.9Q21.5 18.4 21.5 19V20.4Q21.5 21 20.9 21H11.1Q10.5 21 10.5 20.4V19Q10.5 18.4 11.1 18.4ZM11.1 23.6H20.9Q21.5 23.6 21.5 24.2V25.6Q21.5 26.2 20.9 26.2H11.1Q10.5 26.2 10.5 25.6V24.2Q10.5 23.6 11.1 23.6Z"
            />
          </svg>
          <div
            style={{
              fontFamily: 'Bricolage',
              fontWeight: 700,
              fontSize: 84,
              lineHeight: 1.04,
              letterSpacing: -2.5,
              maxWidth: 960,
            }}
          >
            Ship it to a server you own.
          </div>
        </div>
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            padding: '38px 84px',
            background: '#081513',
            borderTop: '6px solid #e39155',
            fontSize: 28,
            color: '#c5d6d1',
          }}
        >
          <div>Small boat, your own water.</div>
          <div>useskiff.xyz</div>
        </div>
      </div>
    ),
    {
      ...size,
      fonts: [
        { name: 'Instrument Sans', data: regular, weight: 400, style: 'normal' },
        { name: 'Bricolage', data: display, weight: 700, style: 'normal' },
      ],
    },
  )
}
