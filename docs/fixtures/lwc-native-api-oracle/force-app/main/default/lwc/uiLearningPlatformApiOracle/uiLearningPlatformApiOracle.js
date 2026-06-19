import { LightningElement } from "lwc";
import * as api from "lightning/uiLearningPlatformApi";

export default class Oracle extends LightningElement {
  label = "lightning/uiLearningPlatformApi";
  exports = Object.keys(api || {}).join(",");
}
