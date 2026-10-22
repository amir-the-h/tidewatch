<!-- Map output: which execution metadata key holds coordinates for /map pins, its format and the popup. -->
<script setup lang="ts">
import { FormField, Input, Select, Textarea } from '@/components/ui'
import FormSection from './FormSection.vue'
import type { ActionFormState } from './actionForm'

defineProps<{ form: ActionFormState }>()

const coordinateTypeOptions = [
  { label: 'Array of pairs  [[lat,lon],...]',           value: 'array_of_pairs'   },
  { label: 'Single pair  [lat,lon]',                    value: 'single_pair'      },
  { label: 'Object  {"lat":x,"lon":x}',                 value: 'object'           },
  { label: 'Array of objects  [{"lat":x,"lon":x},...]', value: 'array_of_objects' },
  { label: 'GeoJSON Point',                             value: 'geojson_point'    },
]
</script>

<template>
  <FormSection title="Map output" description="Optional. Plot this action's coordinates as pins on the map.">
    <FormField label="Coordinate key" for="action-map-key">
      <Input id="action-map-key" v-model="form.mapCoordinateKey" placeholder="e.g. geo_coordinates" clearable class="font-mono" />
    </FormField>
    <template v-if="form.mapCoordinateKey.trim()">
      <FormField label="Coordinate type">
        <Select v-model="form.mapCoordinateType" :options="coordinateTypeOptions" />
      </FormField>
      <FormField
        label="Popup template"
        for="action-map-popup"
        description="${key} for any execution metadata key; built-ins ${tweet_id}, ${tweet_created_at}, ${model_name}. Leave empty for the default layout."
      >
        <Textarea
          id="action-map-popup"
          v-model="form.mapPopupTemplate"
          :rows="3"
          placeholder="${description}&#10;Confidence: ${confidence}"
          class="font-mono"
        />
      </FormField>
    </template>
  </FormSection>
</template>
