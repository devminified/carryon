# CO-506: the one-shot init/upgrade container — applies every
# migration under supabase/migrations/ against this stack's own
# Postgres, then exits 0. docker-compose.yml's own `init` service
# mounts the real supabase/ directory read-only and runs this on
# every `docker compose up`, not just the first one — see apply.sh's
# own comment for why that's what makes an N -> N+1 upgrade safe.
FROM postgres:15-alpine

RUN apk add --no-cache bash

COPY apply.sh /apply.sh
RUN chmod +x /apply.sh

ENTRYPOINT ["/apply.sh"]
