# <reponame>-<slug> — design

## 1. The resource

<The model. One block per table or object, with the id prefix it is addressed
by. State what already exists in the baseline and is reused, and what is new.>

```
<resource>
  id            text        <prefix>_… , unique within <scope>
  org_id        uuid        the owning workspace
  …
  created_at    timestamptz
```

## 2. The API

<One subsection per route group. Method, path, request, response envelope,
authorization rule, error codes. Envelopes follow the baseline's `{ data, meta }`
shape.>

```
POST   /v1/organizations/{org}/<resources>
GET    /v1/organizations/{org}/<resources>
DELETE /v1/organizations/{org}/<resources>/{id}
```

## 3. The console

<Which surfaces change, screen by screen, and what a user can now do on each.>

## 4. Events, secrets, and integrations

<Audit events emitted, webhooks, any provider connection or brokered secret
this needs.>

## 5. Out of scope

<What this epic deliberately does not do, and where that work lives instead.>
