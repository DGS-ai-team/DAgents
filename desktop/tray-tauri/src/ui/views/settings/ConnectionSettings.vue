<script setup>
import { useRoute, useRouter } from "vue-router";
import SetupConfigPanel from "../../components/SetupConfigPanel.vue";

const route = useRoute();
const router = useRouter();

function clearProfileQuery() {
  if (!route.query.profile) return;
  const query = { ...route.query };
  delete query.profile;
  router.replace({ query });
}
</script>

<template>
  <div class="settings-page settings-embedded">
    <SetupConfigPanel
      :requested-profile-id="String(route.query.profile || '')"
      @profile-request-handled="clearProfileQuery"
    />
  </div>
</template>
