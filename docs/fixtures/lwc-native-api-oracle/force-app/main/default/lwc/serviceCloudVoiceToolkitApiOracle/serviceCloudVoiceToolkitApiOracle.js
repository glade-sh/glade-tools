import { LightningElement } from "lwc";
import * as api from "lightning/serviceCloudVoiceToolkitApi";

export default class Oracle extends LightningElement {
  label = "lightning/serviceCloudVoiceToolkitApi";
  exports = Object.keys(api || {}).join(",");
}
