<script setup>
defineProps({
  active: { type: Boolean, default: false },
});
</script>

<template>
  <section class="module-placeholder" aria-label="Agent 模板">
    <div class="module-placeholder__card">
      <p class="module-placeholder__kicker">即将推出</p>
      <h2 class="module-placeholder__title">Agent 模板</h2>
      <p class="module-placeholder__body">
        在此维护可复用的 Agent 蓝图。工作组新增成员时，可选用模板快速填充角色、指令与推荐能力包。
      </p>
      <dl class="module-placeholder__spec">
        <dt>拟定字段</dt>
        <dd>
          <code>template_id</code>、<code>display_name</code>、<code>description</code>、
          <code>system_prompt</code> / <code>instructions</code>、
          <code>recommended_skills[]</code>、<code>recommended_hooks[]</code>、
          <code>recommended_tools[]</code>、<code>default_llm_profile_id</code>
        </dd>
        <dt>与工作组衔接</dt>
        <dd>
          后续 <code>POST /v1/workgroups/&#123;id&#125;/members</code> 支持
          <code>template_id</code>，展开为现有 <code>MemberCreateRequest</code>。
        </dd>
        <dt>与能力市场</dt>
        <dd>推荐的 Skills / Hooks / Tools 引用能力市场中的 package id。</dd>
      </dl>
    </div>
  </section>
</template>
