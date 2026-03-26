<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { listUsers, createUser, updateUser, deleteUser } from '../api/users'
import { useAuthStore } from '../stores/auth'
import type { AuthUser } from '../api/auth'
import { PersonAddOutline, CreateOutline, TrashOutline } from '@vicons/ionicons5'
import IconButton from '../components/IconButton.vue'

const message = useMessage()
const auth    = useAuthStore()

const users   = ref<AuthUser[]>([])
const loading = ref(false)

const filterText = ref('')
const filterRole = ref<string | null>(null)

const filteredUsers = computed(() => {
  let list = users.value
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(u => u.username.toLowerCase().includes(q))
  if (filterRole.value) list = list.filter(u => u.role === filterRole.value)
  return list
})

async function fetchUsers() {
  loading.value = true
  try {
    users.value = await listUsers()
  } catch (e: any) {
    message.error(e?.message ?? 'Failed to load users')
  } finally {
    loading.value = false
  }
}

onMounted(fetchUsers)

// ── add / edit modal ──────────────────────────────────────────────────────────

const showModal  = ref(false)
const editTarget = ref<AuthUser | null>(null)
const saving     = ref(false)

const form = ref({ username: '', password: '', role: 'viewer' as 'admin' | 'viewer' })

const roleOptions = [
  { label: 'Viewer — read-only access', value: 'viewer' },
  { label: 'Admin — full access',       value: 'admin'  },
]

function openAdd() {
  editTarget.value = null
  form.value = { username: '', password: '', role: 'viewer' }
  showModal.value = true
}

function openEdit(u: AuthUser) {
  editTarget.value = u
  form.value = { username: u.username, password: '', role: u.role }
  showModal.value = true
}

async function saveModal() {
  if (!editTarget.value && (!form.value.username || !form.value.password)) {
    message.error('Username and password are required')
    return
  }
  saving.value = true
  try {
    if (editTarget.value) {
      const req: { password?: string; role?: 'admin' | 'viewer' } = { role: form.value.role }
      if (form.value.password) req.password = form.value.password
      await updateUser(editTarget.value.id, req)
      message.success('User updated')
    } else {
      await createUser({ username: form.value.username, password: form.value.password, role: form.value.role })
      message.success('User created')
    }
    showModal.value = false
    fetchUsers()
  } catch (e: any) {
    message.error(e?.message ?? 'Save failed')
  } finally {
    saving.value = false
  }
}

// ── delete ────────────────────────────────────────────────────────────────────

async function remove(u: AuthUser) {
  try {
    await deleteUser(u.id)
    message.success('User deleted')
    fetchUsers()
  } catch (e: any) {
    message.error(e?.message ?? 'Delete failed')
  }
}
</script>

<template>
  <div>
    <PageHeader title="Users" style="margin-bottom: 16px">
      <n-tooltip trigger="hover">
        <template #trigger>
          <n-button type="primary" @click="openAdd">
            <template #icon><n-icon><PersonAddOutline /></n-icon></template>
          </n-button>
        </template>
        Add User
      </n-tooltip>
    </PageHeader>

    <n-flex align="center" :size="8" wrap style="margin-bottom: 12px">
      <n-input v-model:value="filterText" placeholder="Search by username…" clearable style="width: 220px" />
      <n-select
        v-model:value="filterRole"
        :options="[{ label: 'All roles', value: null }, ...roleOptions]"
        clearable placeholder="All roles" style="width: 160px"
      />
    </n-flex>

    <n-spin :show="loading">
      <n-table :bordered="false" :single-line="false" striped>
        <thead>
          <tr>
            <th>Username</th>
            <th style="width: 120px">Role</th>
            <th style="width: 180px">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="filteredUsers.length === 0">
            <td colspan="3" style="text-align: center; color: #888; padding: 32px">No users.</td>
          </tr>
          <tr v-for="u in filteredUsers" :key="u.id">
            <td>
              <n-text strong>{{ u.username }}</n-text>
              <n-tag v-if="u.id === auth.user?.id" size="small" style="margin-left: 8px" :bordered="false">you</n-tag>
            </td>
            <td>
              <n-tag
                size="small"
                :type="u.role === 'admin' ? 'warning' : 'default'"
                :bordered="false"
              >{{ u.role }}</n-tag>
            </td>
            <td>
              <n-space size="small">
                <IconButton :icon="CreateOutline" label="Edit" @click="openEdit(u)" />
                <n-popconfirm @positive-click="remove(u)">
                  <template #trigger>
                    <IconButton :icon="TrashOutline" label="Delete" type="error" ghost :disabled="u.id === auth.user?.id" />
                  </template>
                  Delete user "{{ u.username }}"?
                </n-popconfirm>
              </n-space>
            </td>
          </tr>
        </tbody>
      </n-table>
    </n-spin>

    <!-- Add / Edit modal -->
    <n-modal
      v-model:show="showModal"
      preset="card"
      :title="editTarget ? `Edit — ${editTarget.username}` : 'Add User'"
      style="width: 400px"
    >
      <n-form label-placement="left" label-width="90">
        <n-form-item v-if="!editTarget" label="Username">
          <n-input v-model:value="form.username" placeholder="username" />
        </n-form-item>
        <n-form-item label="Password">
          <n-input
            v-model:value="form.password"
            type="password"
            show-password-on="click"
            :placeholder="editTarget ? 'Leave blank to keep current' : 'Password'"
          />
        </n-form-item>
        <n-form-item label="Role">
          <n-select v-model:value="form.role" :options="roleOptions" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-flex justify="end">
          <n-button @click="showModal = false">Cancel</n-button>
          <n-button type="primary" :loading="saving" @click="saveModal">Save</n-button>
        </n-flex>
      </template>
    </n-modal>
  </div>
</template>
