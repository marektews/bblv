<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError, login } from '../api'
import { start } from '../store'

const { t } = useI18n()
const password = ref('')
const busy = ref(false)
const error = ref('')

async function submit() {
  busy.value = true
  error.value = ''
  try {
    await login(password.value)
    password.value = ''
    await start()
  } catch (err) {
    // Only a refusal means a wrong password; the server may be unreachable.
    if (err instanceof ApiError && err.status === 401) {
      error.value = t('login.failed')
    } else if (err instanceof ApiError) {
      error.value = err.message
    } else {
      error.value = t('app.disconnected')
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="login">
    <el-card>
      <h3><fa-icon icon="lock" class="ic" />{{ t('login.title') }}</h3>
      <el-form @submit.prevent="submit">
        <el-form-item :error="error">
          <el-input v-model="password" type="password" show-password autofocus :placeholder="t('login.password')" />
        </el-form-item>
        <el-button type="primary" native-type="submit" :loading="busy"><fa-icon icon="right-to-bracket" class="ic" />{{ t('login.submit') }}</el-button>
      </el-form>
    </el-card>
  </div>
</template>

<style scoped>
.login {
  display: flex;
  justify-content: center;
  padding-top: 10vh;
}

.el-card {
  width: min(360px, 100%);
}

h3 {
  margin: 0 0 16px;
}
</style>
