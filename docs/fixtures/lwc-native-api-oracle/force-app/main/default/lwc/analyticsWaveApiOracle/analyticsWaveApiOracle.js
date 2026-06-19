import { LightningElement } from "lwc";
import * as api from "lightning/analyticsWaveApi";

export default class Oracle extends LightningElement {
  label = "lightning/analyticsWaveApi";
  exports = Object.keys(api || {}).join(",");
}
