// Connect-Web transport + ControlService client singleton.
//
// The transport hits "/" so the same bundle runs in dev (Vite proxy
// rewrites /nucleus.admin.v1.* to the admin server) and in production
// (admin server serves the UI and the RPC paths from the same origin).

import { createPromiseClient } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import { ControlService } from '@/gen/nucleus/admin/v1/admin_connect.js'

const transport = createConnectTransport({
  baseUrl: '/',
  // JSON over HTTP/1.1 keeps payloads readable in browser devtools and
  // works through any reasonable reverse proxy. Connect-Web negotiates
  // protocol with the server transparently.
  useBinaryFormat: false,
  // Send credentials so cookies set by an auth-aware reverse proxy
  // (oauth2-proxy etc.) are forwarded.
  credentials: 'same-origin',
})

export const controlClient = createPromiseClient(ControlService, transport)
