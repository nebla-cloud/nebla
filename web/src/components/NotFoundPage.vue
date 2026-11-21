<!--
Copyright (C) 2026 The Nebla contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

<script setup lang="ts">
import { RouterLink } from 'vue-router'
import Lockup from './Lockup.vue'
</script>

<template>
  <main
    class="flex min-h-dvh flex-col items-start justify-center gap-5 px-6 py-16 sm:mx-auto sm:max-w-md"
  >
    <Lockup :height="28" />
    <div class="flex flex-col gap-2">
      <h1 class="nb-h1 text-highlighted">
        We can't find that page
      </h1>
      <p class="text-muted">It may have been deleted, or you might not have access to it.</p>
    </div>
    <RouterLink
      :to="{ name: 'projects' }"
      class="font-semibold text-highlighted underline decoration-accented underline-offset-4 hover:decoration-current"
    >
      Go to your projects
    </RouterLink>
  </main>
</template>
