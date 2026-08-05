<script setup>
defineProps({
  active: { type: Boolean, default: false },
});
</script>

<template>
  <section class="module-placeholder" aria-label="权限管理">
    <div class="module-placeholder__card">
      <p class="module-placeholder__kicker">即将推出</p>
      <h2 class="module-placeholder__title">权限管理</h2>
      <p class="module-placeholder__body">
        统一查看与配置控制面身份、工作组访问控制，以及 Node 发现分组可见性。
      </p>
      <dl class="module-placeholder__spec">
        <dt>当前已有能力</dt>
        <dd>
          管理员登录 / Node 会话（<code>/v1/auth/*</code>）、工作组 ACL
          （owners / collaborators）、registry 的 discovery_groups 过滤。
        </dd>
        <dt>本页规划</dt>
        <dd>
          会话与角色一览、工作组 ACL 批量管理、发现组策略、审计日志入口（
          <code>/v1/admin/audit</code>）。
        </dd>
      </dl>
    </div>
  </section>
</template>
