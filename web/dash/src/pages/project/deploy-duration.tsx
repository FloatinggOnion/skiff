import { useEffect, useState } from 'react'
import { fmtDuration } from '@/lib/format'
import { type Deploy } from '@/services/api.service'

// How long a deploy took, counting up while it builds. Deploys from before this was recorded have no end time.
export function DeployDuration({ deploy }: { deploy: Deploy }) {
  const building = deploy.status === 'building'
  const [now, setNow] = useState(() => Date.now() / 1000)
  useEffect(() => {
    if (!building) return
    const t = setInterval(() => setNow(Date.now() / 1000), 1000)
    return () => clearInterval(t)
  }, [building])
  if (building) return <>{fmtDuration(now - deploy.started)}</>
  if (!deploy.finished) return <>—</>
  return <>{fmtDuration(deploy.finished - deploy.started)}</>
}
