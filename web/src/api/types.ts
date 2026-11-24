// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

import type { components } from './schema'

type S = components['schemas']
export type Action = S['Action']
export type ApiToken = S['ApiToken']
export type AdminLocation = S['AdminLocation']
export type Location = S['Location']
export type Invite = S['Invite']
export type InvitePreview = S['InvitePreview']
export type LocationEnrollment = S['LocationEnrollment']
export type Member = S['Member']
export type Passkey = S['Passkey']
export type Project = S['Project']
export type Role = S['Role']
export type Server = S['Server']
export type Snapshot = S['Snapshot']
export type ServerType = S['ServerType']
export type TokenPermission = S['TokenPermission']
export type UiManifest = S['UiManifest']
export type User = S['User']
export type Volume = S['Volume']
export type FloatingIp = S['FloatingIp']
export type PrimaryIp = S['PrimaryIp']
export type GuestConfig = S['GuestConfig']
export type Network = S['Network']
export type NetworkGuestConfig = S['NetworkGuestConfig']
