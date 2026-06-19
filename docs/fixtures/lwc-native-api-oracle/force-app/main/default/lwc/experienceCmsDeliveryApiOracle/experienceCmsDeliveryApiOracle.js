import { LightningElement } from "lwc";
import * as api from "experience/cmsDeliveryApi";

export default class Oracle extends LightningElement {
  label = "experience/cmsDeliveryApi";
  exports = Object.keys(api || {}).join(",");
}
