import type { Metadata } from "next";
import { Callout, Steps, Step } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "Leaving Skiff",
  description:
    "How to take your apps, databases, files and secrets off Skiff and run them on plain Docker.",
  alternates: { canonical: "/docs/leaving" },
};

export default function Leaving() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Guides</p>
      <h1>Leaving Skiff</h1>
      <p className="text-muted text-lg">
        Your apps are Docker images, and your data is in standard formats. Here&apos;s how to take it
        all out and remove Skiff.
      </p>

      <Callout>
        An automated test follows these steps with an app, a Postgres database, a volume, a secret and
        a bucket, then runs them on plain Docker with Skiff gone. MySQL, MongoDB and Redis backups use
        their standard formats too, but that test doesn&apos;t cover them.
      </Callout>

      <h2>Take your data out</h2>
      <Steps>
        <Step n={1} title="Databases">
          Open the database, go to <strong>Backups</strong>, take a backup, then download it. Stop your
          app first if it must not lose writes made after the backup.
        </Step>
        <Step n={2} title="Volumes">
          Open the project&apos;s Settings, use the backups button beside the volume, take a backup and
          download it. It&apos;s a <code>.tar.gz</code> of the volume&apos;s files.
        </Step>
        <Step n={3} title="Environment variables">
          Copy them from the project&apos;s <strong>Environment</strong>{" "}
          page, or fetch them with an API token. Values containing line breaks need handling by hand, because Docker env files
          don&apos;t support them.
        </Step>
      </Steps>
      <CodeBlock label="shell">{`curl -s https://dash.example.com/api/v1/apps/my-app/env \\
  -H "Authorization: Bearer $SKIFF_TOKEN" \\
  | jq -r '.[] | "\\(.key)=\\(.value)"' > my-app.env`}</CodeBlock>
      <p>
        Variables Skiff adds for you, like the address of a linked database, aren&apos;t in that list.
        To see everything an app runs with, use{" "}
        <code>docker inspect -f &apos;{"{{range .Config.Env}}{{println .}}{{end}}"}&apos; CONTAINER</code>{" "}
        on the server. Those addresses point at Skiff&apos;s databases, so change them for your new ones.
      </p>

      <h3>Buckets</h3>
      <p>
        A bucket&apos;s address is private to the server, so copy its files out on the server, through
        the bucket&apos;s own container. Its name is the host in the endpoint shown on the bucket&apos;s
        page, and the keys are shown there too.
      </p>
      <CodeBlock label="on your server">{`docker run --rm --network container:BUCKET_CONTAINER \\
  -v "$PWD/my-bucket:/out" \\
  -e AWS_ACCESS_KEY_ID=ACCESS_KEY -e AWS_SECRET_ACCESS_KEY=SECRET_KEY \\
  -e AWS_DEFAULT_REGION=us-east-1 \\
  amazon/aws-cli:2.27.50 --endpoint-url http://localhost:8333 \\
  s3 sync s3://my-bucket /out`}</CodeBlock>

      <h3>Your app&apos;s image</h3>
      <p>
        You can keep building from your repo. To keep the exact image Skiff built, save it on the server
        and load it wherever it&apos;s going:
      </p>
      <CodeBlock label="on your server">{`docker save skiff-my-app:latest | gzip > my-app.tar.gz
docker load < my-app.tar.gz   # on the new machine`}</CodeBlock>

      <h2>Run it without Skiff</h2>
      <p>These commands bring back a Postgres database, a volume and the app on plain Docker.</p>
      <CodeBlock label="shell">{`docker run -d --name my-db -e POSTGRES_PASSWORD=CHOOSE_ONE postgres:16-alpine
docker exec -i my-db psql -U postgres < orders.sql

docker volume create my-data
docker run --rm -i -v my-data:/data alpine:3.20 tar xzf - -C /data < data.tar.gz

docker run -d --name my-app --env-file my-app.env \\
  -v my-data:/data -p 8080:8080 skiff-my-app:latest`}</CodeBlock>
      <p>
        When loading the database, errors saying <code>role &quot;skiff&quot; does not exist</code> are
        harmless. Your tables then belong to the user that loaded them. Use your database&apos;s major
        version, and connect the two containers on a shared Docker network so the app can reach it.
      </p>
      <p>
        You&apos;ll also need something to replace what the router did: HTTPS, your domains and sending
        traffic to the app.
      </p>

      <h2>Remove Skiff</h2>
      <p>
        Do this last, once your new setup is serving traffic and your DNS points at it. Stopping the
        router stops all public traffic to apps still on this server.
      </p>
      <CodeBlock label="on your server">{`systemctl disable --now skiff-router skiff-panel@7070 skiff-panel@7071
rm /etc/systemd/system/skiff-router.service /etc/systemd/system/skiff-panel@.service
systemctl daemon-reload
rm /usr/local/bin/skiff

# Review these lists, then remove what you no longer need.
docker ps -a --filter label=skiff=1
docker ps -a --filter label=skiff.kind=database
docker volume ls`}</CodeBlock>
      <Callout>
        Deleting a volume deletes its data for good. Check that your copies open before you remove
        anything. <code>~/.skiff</code> holds unencrypted backups and your recovery key, so delete that
        folder when you&apos;re done. Copies in your offsite bucket stay until you delete them.
      </Callout>
    </article>
  );
}
