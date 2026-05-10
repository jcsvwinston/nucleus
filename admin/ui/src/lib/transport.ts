// Connect-Web transport + service client singletons.
//
// The transport hits "/" so the same bundle runs in dev (Vite proxy
// rewrites /nucleus.admin.v1.* to the admin server) and in production
// (admin server serves the UI and the RPC paths from the same origin).

import { createPromiseClient } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import {
  ControlService,
  DataStudioService,
} from '@/gen/nucleus/admin/v1/admin_connect.js'

const transport = createConnectTransport({
  baseUrl: '/',
  useBinaryFormat: false,
  credentials: 'same-origin',
})

export const controlClient = createPromiseClient(ControlService, transport)
export const dataStudioClient = createPromiseClient(DataStudioService, transport)
