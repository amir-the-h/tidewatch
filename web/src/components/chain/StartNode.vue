<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
</script>

<template>
  <div style="
    padding: 8px 16px; border-radius: 20px;
    background: #313244; border: 2px solid #89dceb;
    font-size: 12px; color: #89dceb; font-weight: 600;
    min-width: 80px; text-align: center;
  ">
    Start
    <Handle type="source" :position="Position.Right" id="default" style="background: #89dceb" />
  </div>
</template>
