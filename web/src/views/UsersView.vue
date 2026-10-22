<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { PencilIcon, TrashIcon } from '@lucide/vue'
import {
  AlertDialog, Badge, Button, DataTable, FormField, IconButton, Input, Select, Sheet,
  type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { listUsers, createUser, updateUser, deleteUser } from '../api/users'
import { useAuthStore } from '../stores/auth'
import type { AuthUser } from '../api/auth'
import PageHeader from '../components/PageHeader.vue'

type Role = 'admin' | 'viewer'

const toast = useToast()
const auth  = useAuthStore()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

// ── Data ─────────────────────────────────────────────────────────────────────

const users     = ref<AuthUser[]>([])
const loading   = ref(false)
const loadError = ref('')

async function fetchUsers() {
  loading.value = true
  loadError.value = ''
  try { users.value = await listUsers() }
  catch (e: unknown) { loadError.value = errorMessage(e) }
  finally { loading.value = false }
}

// ── Filters ──────────────────────────────────────────────────────────────────

const roleOptions: { label: string; value: Role }[] = [
  { label: 'Viewer — read-only access', value: 'viewer' },
  { label: 'Admin — full access',       value: 'admin'  },
]

const roleFilterOptions: { label: string; value: Role }[] = [
  { label: 'Viewer', value: 'viewer' },
  { label: 'Admin',  value: 'admin'  },
]

const filterText = ref('')
const filterRole = ref<Role | null>(null)

const filteredUsers = computed(() => {
  let list = users.value
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(u => u.username.toLowerCase().includes(q))
  if (filterRole.value) list = list.filter(u => u.role === filterRole.value)
  return list
})

const hasFilters = computed(() => !!filterText.value.trim() || !!filterRole.value)

function clearFilters() {
  filterText.value = ''
  filterRole.value = null
}

// ── Create / edit ────────────────────────────────────────────────────────────

const showForm   = ref(false)
const editTarget = ref<AuthUser | null>(null)
const saving     = ref(false)
const form       = ref({ username: '', password: '', role: 'viewer' as Role })

function openCreate() {
  editTarget.value = null
  form.value = { username: '', password: '', role: 'viewer' }
  showForm.value = true
}

function openEdit(u: AuthUser) {
  editTarget.value = u
  form.value = { username: u.username, password: '', role: u.role }
  showForm.value = true
}

async function onSave() {
  if (!editTarget.value && (!form.value.username || !form.value.password)) {
    toast.error('Enter a username and password')
    return
  }
  saving.value = true
  try {
    if (editTarget.value) {
      const req: { password?: string; role?: Role } = { role: form.value.role }
      if (form.value.password) req.password = form.value.password
      await updateUser(editTarget.value.id, req)
      toast.success('User updated')
    } else {
      await createUser({ username: form.value.username, password: form.value.password, role: form.value.role })
      toast.success('User created')
    }
    showForm.value = false
    fetchUsers()
  } catch (e: unknown) {
    toast.error(`Could not ${editTarget.value ? 'update' : 'create'} user: ${errorMessage(e)}`)
  } finally {
    saving.value = false
  }
}

// ── Delete ───────────────────────────────────────────────────────────────────

const deleteTarget = ref<AuthUser | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

function askDelete(u: AuthUser) {
  deleteTarget.value = u
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await deleteUser(target.id)
    toast.success('User deleted')
    showDelete.value = false
    fetchUsers()
  } catch (e: unknown) {
    toast.error(`Could not delete user: ${errorMessage(e)}`)
  } finally {
    deleting.value = false
  }
}

// ── Table columns ────────────────────────────────────────────────────────────

const columns: DataTableColumns<AuthUser> = [
  {
    title: 'ID', key: 'id', width: 64, sorter: 'default',
    render: (row) => h('span', { class: 'font-mono tabular text-muted-foreground' }, String(row.id)),
  },
  {
    title: 'Username', key: 'username', sorter: 'default', ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'inline-flex items-center gap-2' }, [
      h('span', { class: 'font-medium' }, row.username),
      row.id === auth.user?.id ? h(Badge, { variant: 'outline' }, () => 'You') : null,
    ]),
  },
  {
    title: 'Role', key: 'role', width: 120, sorter: 'default',
    render: (row) => h(Badge, { variant: 'secondary' }, () => row.role === 'admin' ? 'Admin' : 'Viewer'),
  },
  {
    title: '', key: 'actions', width: 90, align: 'right',
    render: (row) => h('div', { class: 'flex items-center justify-end gap-1' }, [
      h(IconButton, { icon: PencilIcon, label: 'Edit', onClick: () => openEdit(row) }),
      h(IconButton, {
        icon: TrashIcon,
        label: row.id === auth.user?.id ? 'You can\'t delete your own account' : 'Delete',
        disabled: row.id === auth.user?.id,
        onClick: () => askDelete(row),
      }),
    ]),
  },
]

onMounted(fetchUsers)
</script>

<template>
  <div class="flex flex-col gap-3">
    <PageHeader
      title="Users"
      description="Who can sign in. Admins have full access; viewers see the map, tweets and tracker."
      add-label="New user"
      @add="openCreate"
    />

    <div class="flex flex-wrap items-center gap-2">
      <div class="w-64">
        <Input v-model="filterText" placeholder="Search username" clearable />
      </div>
      <div class="w-44">
        <Select v-model="filterRole" :options="roleFilterOptions" placeholder="All roles" clearable />
      </div>
    </div>

    <div
      v-if="loadError"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span class="text-destructive">Could not load users: {{ loadError }}</span>
      <Button variant="outline" @click="fetchUsers">Retry</Button>
    </div>

    <DataTable
      v-else
      :columns="columns"
      :data="filteredUsers"
      :loading="loading"
      :row-key="(row: AuthUser) => row.id"
      :page-size="20"
    >
      <template #empty>
        <div v-if="hasFilters && users.length" class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No users match these filters.</p>
          <Button variant="outline" @click="clearFilters">Clear filters</Button>
        </div>
        <div v-else class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No users yet. Add one to give someone access.</p>
          <Button @click="openCreate">New user</Button>
        </div>
      </template>
    </DataTable>
  </div>

  <!-- Create / edit user -->
  <Sheet
    v-model:open="showForm"
    :title="editTarget ? `Edit ${editTarget.username}` : 'New user'"
    :description="editTarget ? 'Change the role, or set a new password.' : 'The user signs in with this username and password.'"
  >
    <form class="flex flex-col gap-3 p-4" @submit.prevent="onSave">
      <FormField v-if="!editTarget" label="Username" for="user-username" required>
        <Input id="user-username" v-model="form.username" placeholder="e.g. analyst" autocomplete="off" />
      </FormField>
      <FormField
        label="Password"
        for="user-password"
        :required="!editTarget"
        :description="editTarget ? 'Leave blank to keep the current password.' : undefined"
      >
        <Input
          id="user-password"
          v-model="form.password"
          type="password"
          autocomplete="new-password"
          :placeholder="editTarget ? 'Unchanged' : undefined"
        />
      </FormField>
      <FormField label="Role" required>
        <Select v-model="form.role" :options="roleOptions" />
      </FormField>
    </form>
    <template #footer>
      <Button variant="outline" size="default" @click="showForm = false">Cancel</Button>
      <Button size="default" :loading="saving" @click="onSave">
        {{ editTarget ? 'Save user' : 'Create user' }}
      </Button>
    </template>
  </Sheet>

  <AlertDialog
    v-model:open="showDelete"
    :title="`Delete ${deleteTarget?.username ?? 'user'}?`"
    description="The user account is removed. This can't be undone."
    action-label="Delete user"
    :loading="deleting"
    @confirm="onConfirmDelete"
  />
</template>
